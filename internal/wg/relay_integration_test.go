package wg_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/wg"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

type relayClient struct {
	net      *netstack.Net
	key      wg.Key
	ip4, ip6 netip.Addr
	received chan []byte
}

type portableTestBind struct{ conn.Bind }

// Observe authenticated packets before the client netstack consumes them. Its
// PingConn can miss a notification when a reply arrives before Read registers.
// The buffered observer validates actual replies without that timing dependency.
type observedTUN struct {
	tun.Device
	received chan []byte
}

func (o *observedTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		select {
		case o.received <- append([]byte(nil), b[offset:]...):
		default:
		}
	}
	return o.Device.Write(bufs, offset)
}

// Two clients exchange real encrypted WireGuard traffic over host UDP sockets.
// Windows also verifies Wintun host access; Linux needs no driver/elevation.
// Neither path needs WinNAT or a public Internet endpoint.
func TestRelayEncryptedTraffic(t *testing.T) {
	prepareRelayTest(t)
	// A fresh Windows ARM64 runner can spend over 40 seconds installing its
	// first driver and provisioning firewall rules before packets are tested.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	be, err := wg.NewRelay("relay-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := wg.GeneratePrivateKey()
	if err = be.Up(ctx, wg.DeviceConfig{PrivateKey: key}, []netip.Prefix{netip.MustParsePrefix("10.254.249.1/24"), netip.MustParsePrefix("fd20:249::1/64")}, 1420); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := be.Down(context.Background()); err != nil {
			t.Error(err)
		}
	})
	state, err := be.Device(ctx)
	if err != nil || state.ListenPort == 0 {
		t.Fatalf("listen port: %+v %v", state, err)
	}
	prepareRelayNetwork(t, be, state.ListenPort)
	var clients []relayClient
	for n := 2; n <= 3; n++ {
		priv, _ := wg.GeneratePrivateKey()
		psk, _ := wg.GeneratePrivateKey()
		ip4, ip6 := netip.MustParseAddr(fmt.Sprintf("10.254.249.%d", n)), netip.MustParseAddr(fmt.Sprintf("fd20:249::%d", n))
		if err = be.SetPeer(ctx, wg.PeerConfig{PublicKey: priv.PublicKey(), PresharedKey: &psk, AllowedIPs: []netip.Prefix{netip.PrefixFrom(ip4, 32), netip.PrefixFrom(ip6, 128)}}); err != nil {
			t.Fatal(err)
		}
		tun, client, err := netstack.CreateNetTUN([]netip.Addr{ip4, ip6}, nil, 1420)
		if err != nil {
			t.Fatal(err)
		}
		observed := &observedTUN{Device: tun, received: make(chan []byte, 1024)}
		d := device.NewDevice(observed, &portableTestBind{conn.NewDefaultBind()}, device.NewLogger(device.LogLevelError, "relay-client: "))
		t.Cleanup(d.Close)
		pub := key.PublicKey()
		text := fmt.Sprintf("private_key=%s\npublic_key=%s\npreshared_key=%x\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n", hex.EncodeToString(priv[:]), hex.EncodeToString(pub[:]), psk[:], state.ListenPort)
		if err = d.IpcSet(text); err != nil {
			t.Fatal(err)
		}
		if err = d.Up(); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, relayClient{net: client, key: priv, ip4: ip4, ip6: ip6, received: observed.received})
	}
	// Both roaming endpoints must first authenticate to the server, as normal
	// WireGuard clients do with their PersistentKeepalive setting.
	for _, client := range clients {
		relayPing(t, client, netip.MustParseAddr("10.254.249.1"))
	}
	checkRelayHostAccess(t, clients[0])

	for _, ip := range []netip.Addr{clients[1].ip4, clients[1].ip6} {
		t.Run("peer TCP "+ip.String(), func(t *testing.T) {
			ln, err := clients[1].net.ListenTCPAddrPort(netip.AddrPortFrom(ip, 3389))
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				c, err := ln.Accept()
				if err == nil {
					defer c.Close()
					_, _ = io.Copy(c, c)
				}
			}()
			c, err := clients[0].net.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(ip, 3389))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundTrip(t, c, []byte("VPN20 peer TCP/RDP stream"))
		})
		t.Run("peer UDP "+ip.String(), func(t *testing.T) {
			ln, err := clients[1].net.ListenUDPAddrPort(netip.AddrPortFrom(ip, 5353))
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				b := make([]byte, 65535)
				for {
					n, from, err := ln.ReadFrom(b)
					if err != nil {
						return
					}
					_, _ = ln.WriteTo(b[:n], from)
				}
			}()
			c, err := clients[0].net.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(ip, 5353))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundTrip(t, c, []byte("VPN20 peer UDP"))
		})
		t.Run("peer ping "+ip.String(), func(t *testing.T) { relayPing(t, clients[0], ip) })
	}
	for _, ip := range []netip.Addr{netip.MustParseAddr("10.254.249.1"), netip.MustParseAddr("fd20:249::1")} {
		t.Run("server ping "+ip.String(), func(t *testing.T) { relayPing(t, clients[0], ip) })
	}
	var host netip.Addr
	vpnSubnet := netip.MustParsePrefix("10.254.249.0/24")
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		host = c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
		_ = c.Close()
	}
	if !host.IsGlobalUnicast() || vpnSubnet.Contains(host) {
		host = netip.Addr{}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, address := range addrs {
		if host.IsValid() {
			break
		}
		p, err := netip.ParsePrefix(address.String())
		if err == nil && p.Addr().Is4() && p.Addr().IsGlobalUnicast() && !vpnSubnet.Contains(p.Addr()) {
			host = p.Addr()
			break
		}
	}
	t.Logf("physical host IPv4 for egress checks: %s", host)
	t.Run("host TCP source and half close", func(t *testing.T) {
		if !host.IsValid() {
			t.Skip("no host network adapter in this environment; required on Windows CI")
		}
		ln, err := net.Listen("tcp4", net.JoinHostPort(host.String(), "0"))
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		seen := make(chan string, 1)
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
			seen <- c.RemoteAddr().String()
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			b, _ := io.ReadAll(c)
			_, _ = c.Write(append(b, []byte(" after FIN")...))
		}()
		port := uint16(ln.Addr().(*net.TCPAddr).Port)
		c, err := clients[0].net.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(host, port))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = c.Write([]byte("host TCP"))
		_ = c.CloseWrite()
		b, err := io.ReadAll(c)
		if err != nil || string(b) != "host TCP after FIN" {
			t.Fatalf("half-close reply: %q %v", b, err)
		}
		source := <-seen
		if ip, _, _ := net.SplitHostPort(source); ip != host.String() {
			t.Fatalf("LAN saw %s, wanted host address %s", source, host)
		}
	})
	t.Run("host UDP concurrent clients and datagrams", func(t *testing.T) {
		if !host.IsValid() {
			t.Skip("no host network adapter in this environment; required on Windows CI")
		}
		ln, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(host, 0)))
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		seen := make(chan netip.Addr, 20)
		go func() {
			b := make([]byte, 65535)
			for {
				n, from, err := ln.ReadFromUDPAddrPort(b)
				if err != nil {
					return
				}
				seen <- from.Addr()
				_, _ = ln.WriteToUDPAddrPort(b[:n], from)
			}
		}()
		for _, client := range clients {
			c, err := client.net.DialUDPAddrPort(netip.AddrPort{}, ln.LocalAddr().(*net.UDPAddr).AddrPort())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, size := range []int{0, 10, 5000, 20000} {
				roundTrip(t, c, bytes.Repeat([]byte{byte(size)}, size))
				if ip := <-seen; ip != host {
					t.Fatalf("UDP source %s is not host %s", ip, host)
				}
			}
		}
	})
	t.Run("isolation applies live to both IP families", func(t *testing.T) {
		forwarder := be.(wg.Forwarder)
		if err := forwarder.SetForwarding(ctx, wg.ForwardingConfig{PeerIsolation: true, ClampMSS: true}); err != nil {
			t.Fatal(err)
		}
		for _, ip := range []netip.Addr{clients[1].ip4, clients[1].ip6} {
			if pingReply(t, clients[0], ip, 200*time.Millisecond) {
				t.Fatal("isolated peers can still exchange packets")
			}
		}
		if err := forwarder.SetForwarding(ctx, wg.ForwardingConfig{ClampMSS: true}); err != nil {
			t.Fatal(err)
		}
		relayPing(t, clients[0], clients[1].ip4)
	})
	state, err = be.Device(ctx)
	if err != nil || len(state.Peers) != 2 || state.Peers[0].LastHandshake.IsZero() || state.Peers[0].ReceiveBytes == 0 || state.Peers[0].TransmitBytes == 0 {
		t.Fatalf("relay telemetry: %+v %v", state, err)
	}
	if err = be.SetMTU(ctx, 1380); err != nil {
		t.Fatal(err)
	}
	if err = be.RemovePeer(ctx, clients[1].key.PublicKey()); err != nil {
		t.Fatal(err)
	}
	state, err = be.Device(ctx)
	if err != nil || len(state.Peers) != 1 {
		t.Fatalf("remove peer: %+v %v", state, err)
	}
	if err = be.ReplacePeers(ctx, nil); err != nil {
		t.Fatal(err)
	}
	state, err = be.Device(ctx)
	if err != nil || len(state.Peers) != 0 {
		t.Fatalf("replace peers: %+v %v", state, err)
	}
}

func roundTrip(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, max(len(payload), 1))
	n, err := c.Read(buf)
	if err != nil || !bytes.Equal(buf[:n], payload) {
		t.Fatalf("round trip (%d bytes): got %d, %v", len(payload), n, err)
	}
}

func relayPing(t *testing.T, client relayClient, ip netip.Addr) {
	t.Helper()
	if !pingReply(t, client, ip, 5*time.Second) {
		t.Fatalf("no authenticated echo reply from %s", ip)
	}
}

func pingReply(t *testing.T, client relayClient, ip netip.Addr, timeout time.Duration) bool {
	t.Helper()
	c, err := client.net.DialPingAddr(netip.Addr{}, ip)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var typ icmp.Type = ipv4.ICMPTypeEcho
	if ip.Is6() {
		typ = ipv6.ICMPTypeEchoRequest
	}
	payload := []byte(fmt.Sprintf("encrypted ping %d", time.Now().UnixNano()))
	msg, _ := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: 0x2020, Seq: 1, Data: payload}}).Marshal(nil)
	if _, err = c.Write(msg); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return false
		case packet := <-client.received:
			offset, replyType := 20, byte(0)
			if ip.Is6() {
				offset, replyType = 40, 129
				if len(packet) < 48 || packet[0]>>4 != 6 || packet[6] != 58 || netip.AddrFrom16([16]byte(packet[8:24])) != ip {
					continue
				}
			} else {
				if len(packet) < 28 || packet[0]>>4 != 4 || packet[9] != 1 || netip.AddrFrom4([4]byte(packet[12:16])) != ip {
					continue
				}
				offset = int(packet[0]&15) * 4
			}
			if len(packet) >= offset+8 && packet[offset] == replyType && bytes.Equal(packet[offset+8:], payload) {
				return true
			}
		}
	}
}
