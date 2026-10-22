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
	"golang.zx2c4.com/wireguard/tun/netstack"
)

type relayClient struct {
	net      *netstack.Net
	key      wg.Key
	ip4, ip6 netip.Addr
}

type portableTestBind struct{ conn.Bind }

// Two clients exchange real encrypted WireGuard traffic over host UDP sockets.
// Windows also verifies Wintun host access; Linux needs no driver/elevation.
// Neither path needs WinNAT or a public Internet endpoint.
func TestRelayEncryptedTraffic(t *testing.T) {
	prepareRelayTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
		d := device.NewDevice(tun, &portableTestBind{conn.NewDefaultBind()}, device.NewLogger(device.LogLevelError, "relay-client: "))
		t.Cleanup(d.Close)
		pub := key.PublicKey()
		text := fmt.Sprintf("private_key=%s\npublic_key=%s\npreshared_key=%x\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n", hex.EncodeToString(priv[:]), hex.EncodeToString(pub[:]), psk[:], state.ListenPort)
		if err = d.IpcSet(text); err != nil {
			t.Fatal(err)
		}
		if err = d.Up(); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, relayClient{client, priv, ip4, ip6})
	}
	// Both roaming endpoints must first authenticate to the server, as normal
	// WireGuard clients do with their PersistentKeepalive setting.
	for _, client := range clients {
		relayPing(t, client.net, netip.MustParseAddr("10.254.249.1"))
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
		t.Run("peer ping "+ip.String(), func(t *testing.T) { relayPing(t, clients[0].net, ip) })
	}
	for _, ip := range []netip.Addr{netip.MustParseAddr("10.254.249.1"), netip.MustParseAddr("fd20:249::1")} {
		t.Run("server ping "+ip.String(), func(t *testing.T) { relayPing(t, clients[0].net, ip) })
	}
	var host netip.Addr
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		host = c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
		_ = c.Close()
	}
	addrs, _ := net.InterfaceAddrs()
	for _, address := range addrs {
		p, err := netip.ParsePrefix(address.String())
		if err == nil && p.Addr().Is4() && p.Addr().IsGlobalUnicast() {
			host = p.Addr()
			break
		}
	}
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
			c, err := clients[0].net.DialPingAddr(netip.Addr{}, ip)
			if err != nil {
				t.Fatal(err)
			}
			_ = c.SetDeadline(time.Now().Add(200 * time.Millisecond))
			var typ icmp.Type = ipv4.ICMPTypeEcho
			if ip.Is6() {
				typ = ipv6.ICMPTypeEchoRequest
			}
			packet, _ := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: 0x2020, Seq: 1, Data: []byte("isolated")}}).Marshal(nil)
			_, _ = c.Write(packet)
			b := make([]byte, 100)
			if _, err = c.Read(b); err == nil {
				t.Fatal("isolated peers can still exchange packets")
			}
			_ = c.Close()
		}
		if err := forwarder.SetForwarding(ctx, wg.ForwardingConfig{ClampMSS: true}); err != nil {
			t.Fatal(err)
		}
		relayPing(t, clients[0].net, clients[1].ip4)
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

func relayPing(t *testing.T, client *netstack.Net, ip netip.Addr) {
	t.Helper()
	c, err := client.DialPingAddr(netip.Addr{}, ip)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var typ icmp.Type = ipv4.ICMPTypeEcho
	if ip.Is6() {
		typ = ipv6.ICMPTypeEchoRequest
	}
	msg, _ := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: 0x2020, Seq: 1, Data: []byte("encrypted ping")}}).Marshal(nil)
	if _, err = c.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil || n < 8 || !bytes.Equal(buf[8:n], []byte("encrypted ping")) {
		t.Fatalf("ping %s: %x %v", ip, buf[:n], err)
	}
}
