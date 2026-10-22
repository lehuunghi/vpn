//go:build windows

package wg_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/netcfg"
	"github.com/lehuunghi/vpn/internal/wg"
	"golang.org/x/net/dns/dnsmessage"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// These tests require elevation and the released, signed WireGuardNT DLL.
// They exercise real encrypted packets, WinNAT and WFP, not the mock backend.
func TestWindowsNativeTunnel(t *testing.T) {
	if os.Getenv("VPN20_WINDOWS_INTEGRATION") != "1" {
		t.Skip("native driver tests run in Windows release CI")
	}
	source, err := os.ReadFile(os.Getenv("VPN20_DRIVER_DLL"))
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(exe), "wireguard.dll"), source, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN20_DATA_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	be, err := wg.NewKernel("vpn20test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	portSocket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := portSocket.LocalAddr().(*net.UDPAddr).Port
	_ = portSocket.Close()
	if err = be.Up(ctx, wg.DeviceConfig{PrivateKey: key, ListenPort: port}, []netip.Prefix{netip.MustParsePrefix("10.254.250.1/24")}, 1420); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := be.Down(context.Background()); err != nil {
			t.Error(err)
		}
	})
	egress, err := netcfg.DefaultEgress()
	if err != nil {
		t.Fatal(err)
	}
	rules := netcfg.Rules{Iface: "vpn20test", WindowsMode: "routed", ListenPort: port, Subnets: []netip.Prefix{netip.MustParsePrefix("10.254.250.0/24")}}
	t.Cleanup(func() {
		if err := netcfg.Remove(context.Background(), ""); err != nil {
			t.Error(err)
		}
	})
	apply := func() {
		t.Helper()
		err := netcfg.Apply(ctx, rules)
		var notice *netcfg.NetworkNotice
		if rules.WindowsMode == "routed" {
			if !errors.As(err, &notice) || notice.Mode != "routed" {
				t.Fatalf("expected working routed mode: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	apply()

	newClient := func(ip string) *memoryTUN {
		priv, err := wg.GeneratePrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		if err = be.SetPeer(ctx, wg.PeerConfig{PublicKey: priv.PublicKey(), AllowedIPs: []netip.Prefix{netip.MustParsePrefix(ip + "/32")}}); err != nil {
			t.Fatal(err)
		}
		tun := newMemoryTUN()
		d := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "test: "))
		t.Cleanup(d.Close)
		pub := key.PublicKey()
		cfg := fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\n", hex.EncodeToString(priv[:]), hex.EncodeToString(pub[:]), port)
		if err = d.IpcSet(cfg); err != nil {
			t.Fatal(err)
		}
		if err = d.Up(); err != nil {
			t.Fatal(err)
		}
		return tun
	}
	c1 := newClient("10.254.250.2")
	c2 := newClient("10.254.250.3")
	// Local UDP echo checks handshake, adapter addressing, host firewall,
	// decryption and encryption in both directions.
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("10.254.250.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := listener.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = listener.WriteToUDP(buf[:n], from)
		}
	}()
	for _, client := range []struct {
		tun *memoryTUN
		ip  string
	}{{c1, "10.254.250.2"}, {c2, "10.254.250.3"}} {
		echoed := false
		for attempt := 0; attempt < 5; attempt++ {
			client.tun.inject <- udpPacket(client.ip, "10.254.250.1", 32000, uint16(listener.LocalAddr().(*net.UDPAddr).Port), []byte("VPN20 native echo"))
			if _, ok := waitPayload(client.tun, []byte("VPN20 native echo"), 2*time.Second); ok {
				echoed = true
				break
			}
		}
		if !echoed {
			state, stateErr := be.Device(context.Background())
			t.Fatalf("encrypted UDP round trip failed for %s after retries; driver state=%+v error=%v", client.ip, state, stateErr)
		}
	}
	t.Log("PASS: real WireGuardNT handshake and bidirectional UDP")
	// Verify actual forwarding between two encrypted peers, then WFP isolation.
	c1.inject <- udpPacket("10.254.250.2", "10.254.250.3", 32000, 32001, []byte("peer-open"))
	if _, ok := waitPayload(c2, []byte("peer-open"), 5*time.Second); !ok {
		t.Fatal("peer forwarding failed")
	}
	// ICMP echo and the TCP SYN/SYN-ACK used by RDP must work without WinNAT.
	for _, protocol := range []byte{1, 6} {
		c1.inject <- peerPacket("10.254.250.2", "10.254.250.3", protocol, false)
		if !waitPeerPacket(c2, protocol, false) {
			t.Fatalf("routed protocol %d request failed", protocol)
		}
		c2.inject <- peerPacket("10.254.250.3", "10.254.250.2", protocol, true)
		if !waitPeerPacket(c1, protocol, true) {
			t.Fatalf("routed protocol %d reply failed", protocol)
		}
	}
	t.Log("PASS: routed peer ICMP echo and TCP 3389 SYN/SYN-ACK without NAT or uplink")
	rules.PeerIsolation = true
	apply()
	c1.inject <- udpPacket("10.254.250.2", "10.254.250.3", 32000, 32001, []byte("peer-blocked"))
	if _, ok := waitPayload(c2, []byte("peer-blocked"), 2*time.Second); ok {
		t.Fatal("peer isolation did not block forwarding")
	}
	t.Log("PASS: WFP blocks traffic between VPN peers")
	rules.WindowsMode = "nat"
	rules.Egress = egress
	apply()
	// Resolve through a DNS server on the physical uplink to prove WinNAT
	// and return traffic work beyond the tunnel, independently of the UI.
	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	dnsOut, err := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf("(Get-DnsClientServerAddress -InterfaceAlias '%s' -AddressFamily IPv4).ServerAddresses | Select-Object -First 1", strings.ReplaceAll(egress, "'", "''"))).Output()
	if err != nil {
		t.Fatal(err)
	}
	dnsAddr := strings.TrimSpace(string(dnsOut))
	if dnsAddr == "" {
		dnsAddr = "1.1.1.1"
	}
	name, _ := dnsmessage.NewName("example.com.")
	query, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 0x2020, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	var answer []byte
	for n := 0; n < 4; n++ {
		c1.inject <- udpPacket("10.254.250.2", dnsAddr, 32002, 53, query)
		if packet, ok := waitUDPPort(c1, 32002, 4*time.Second); ok {
			answer = packet
			break
		}
	}
	if len(answer) == 0 {
		t.Fatalf("WinNAT DNS round trip through %s failed", dnsAddr)
	}
	var response dnsmessage.Message
	ihl := int(answer[0]&15) * 4
	if err = response.Unpack(answer[ihl+8:]); err != nil || !response.Response || response.ID != 0x2020 || response.RCode != dnsmessage.RCodeSuccess {
		t.Fatalf("unexpected NAT DNS response: %+v %v", response.Header, err)
	}
	t.Log("PASS: real DNS request and response through WinNAT")
	state, err := be.Device(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Peers) != 2 || state.Peers[0].LastHandshake.IsZero() || state.Peers[0].ReceiveBytes == 0 {
		t.Fatalf("missing native counters: %+v", state)
	}
	if err = be.RemovePeer(ctx, state.Peers[0].PublicKey); err != nil {
		t.Fatal(err)
	}
	state, err = be.Device(ctx)
	if err != nil || len(state.Peers) != 1 {
		t.Fatalf("remove peer failed: %+v %v", state, err)
	}
	if err = be.SetMTU(ctx, 1400); err != nil {
		t.Fatal(err)
	}
	if err = be.ReplacePeers(ctx, nil); err != nil {
		t.Fatal(err)
	}
	state, err = be.Device(ctx)
	if err != nil || len(state.Peers) != 0 {
		t.Fatalf("replace peers failed: %+v %v", state, err)
	}
}

func checksum(p []byte) uint16 {
	var sum uint32
	for len(p) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(p))
		p = p[2:]
	}
	if len(p) != 0 {
		sum += uint32(p[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func peerPacket(src, dst string, protocol byte, reply bool) []byte {
	size := 8
	if protocol == 6 {
		size = 20
	}
	p := make([]byte, 20+size)
	p[0], p[8], p[9] = 0x45, 64, protocol
	binary.BigEndian.PutUint16(p[2:], uint16(len(p)))
	copy(p[12:16], net.ParseIP(src).To4())
	copy(p[16:20], net.ParseIP(dst).To4())
	binary.BigEndian.PutUint16(p[10:], checksum(p[:20]))
	segment := p[20:]
	if protocol == 1 {
		segment[0] = 8
		if reply {
			segment[0] = 0
		}
		binary.BigEndian.PutUint16(segment[4:], 0x2020)
		binary.BigEndian.PutUint16(segment[6:], 1)
		binary.BigEndian.PutUint16(segment[2:], checksum(segment))
	} else {
		sport, dport := uint16(32003), uint16(3389)
		segment[12], segment[13] = 0x50, 2
		if reply {
			sport, dport = dport, sport
			segment[13] = 18
			binary.BigEndian.PutUint32(segment[8:], 1)
		}
		binary.BigEndian.PutUint16(segment, sport)
		binary.BigEndian.PutUint16(segment[2:], dport)
		binary.BigEndian.PutUint16(segment[14:], 65535)
		pseudo := append([]byte(nil), p[12:20]...)
		pseudo = append(pseudo, 0, protocol, 0, byte(size))
		pseudo = append(pseudo, segment...)
		binary.BigEndian.PutUint16(segment[16:], checksum(pseudo))
	}
	return p
}

func waitPeerPacket(tun *memoryTUN, protocol byte, reply bool) bool {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return false
		case p := <-tun.receive:
			if len(p) < 28 || p[9] != protocol {
				continue
			}
			i := int(p[0]&15) * 4
			if protocol == 1 && len(p) >= i+8 {
				want := byte(8)
				if reply {
					want = 0
				}
				if p[i] == want && binary.BigEndian.Uint16(p[i+4:]) == 0x2020 {
					return true
				}
			}
			if protocol == 6 && len(p) >= i+20 {
				want := byte(2)
				if reply {
					want = 18
				}
				if p[i+13] == want {
					return true
				}
			}
		}
	}
}

type memoryTUN struct {
	inject, receive chan []byte
	events          chan tun.Event
	done            chan struct{}
	once            sync.Once
}

func newMemoryTUN() *memoryTUN {
	t := &memoryTUN{inject: make(chan []byte, 20), receive: make(chan []byte, 100), events: make(chan tun.Event, 1), done: make(chan struct{})}
	t.events <- tun.EventUp
	return t
}
func (t *memoryTUN) File() *os.File           { return nil }
func (t *memoryTUN) MTU() (int, error)        { return 1420, nil }
func (t *memoryTUN) Name() (string, error)    { return "memory-test", nil }
func (t *memoryTUN) Events() <-chan tun.Event { return t.events }
func (t *memoryTUN) BatchSize() int           { return 1 }
func (t *memoryTUN) Close() error             { t.once.Do(func() { close(t.done); close(t.events) }); return nil }
func (t *memoryTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case <-t.done:
		return 0, os.ErrClosed
	case packet := <-t.inject:
		sizes[0] = copy(bufs[0][offset:], packet)
		return 1, nil
	}
}
func (t *memoryTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, buf := range bufs {
		packet := append([]byte(nil), buf[offset:]...)
		select {
		case t.receive <- packet:
		case <-t.done:
			return 0, os.ErrClosed
		}
	}
	return len(bufs), nil
}
func waitPayload(t *memoryTUN, want []byte, timeout time.Duration) ([]byte, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return nil, false
		case p := <-t.receive:
			if len(p) >= 28 && p[9] == 17 {
				ihl := int(p[0]&15) * 4
				if len(p) >= ihl+8 && string(p[ihl+8:]) == string(want) {
					return p, true
				}
			}
		}
	}
}
func waitUDPPort(t *memoryTUN, port uint16, timeout time.Duration) ([]byte, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return nil, false
		case p := <-t.receive:
			if len(p) >= 28 && p[9] == 17 {
				ihl := int(p[0]&15) * 4
				if len(p) >= ihl+8 && binary.BigEndian.Uint16(p[ihl+2:]) == port {
					return p, true
				}
			}
		}
	}
}
func udpPacket(src, dst string, sport, dport uint16, payload []byte) []byte {
	p := make([]byte, 28+len(payload))
	p[0] = 0x45
	p[8] = 64
	p[9] = 17
	binary.BigEndian.PutUint16(p[2:], uint16(len(p)))
	copy(p[12:16], net.ParseIP(src).To4())
	copy(p[16:20], net.ParseIP(dst).To4())
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(p[i:]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(p[10:], ^uint16(sum))
	binary.BigEndian.PutUint16(p[20:], sport)
	binary.BigEndian.PutUint16(p[22:], dport)
	binary.BigEndian.PutUint16(p[24:], uint16(8+len(payload)))
	copy(p[28:], payload)
	return p
}
