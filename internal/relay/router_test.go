package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

func testRouter(t *testing.T) (*Router, *netstack.Net, <-chan []byte) {
	t.Helper()
	r, err := New("test-relay", []netip.Prefix{netip.MustParsePrefix("10.20.0.1/24"), netip.MustParsePrefix("fd20::1/64")}, 1420)
	if err != nil {
		t.Fatal(err)
	}
	r.SetPeers([]netip.Addr{netip.MustParseAddr("10.20.0.2"), netip.MustParseAddr("fd20::2")})
	client, n, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.20.0.2"), netip.MustParseAddr("fd20::2")}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	packets := make(chan []byte, 1024)
	bridge := func(dst, src tun.Device) {
		b := make([]byte, 65535)
		for {
			sizes := []int{0}
			_, err := src.Read([][]byte{b}, sizes, 0)
			if err != nil {
				return
			}
			if src == r {
				select {
				case packets <- append([]byte(nil), b[:sizes[0]]...):
				default:
				}
			}
			if _, err = dst.Write([][]byte{b[:sizes[0]]}, 0); err != nil {
				return
			}
		}
	}
	go bridge(r, client)
	go bridge(client, r)
	t.Cleanup(func() { _ = client.Close(); _ = r.Close() })
	return r, n, packets
}

// Loopback listeners make this runnable in an isolated build container. Only
// host dialing is redirected; actual IPv4/IPv6 packets, fragmentation, TCP,
// checksums, stream half-close, UDP flow state and cleanup use production code.
func TestHostRelayBothFamilies(t *testing.T) {
	for _, family := range []string{"4", "6"} {
		t.Run(family, func(t *testing.T) {
			r, client, _ := testRouter(t)
			loopback, remote := "127.0.0.1", netip.MustParseAddr("203.0.113.5")
			if family == "6" {
				loopback, remote = "::1", netip.MustParseAddr("2001:db8::5")
			}
			r.hostDial = func(d *net.Dialer, network, address string) (net.Conn, error) {
				if !strings.HasSuffix(network, family) {
					return nil, errors.New("wrong outbound address family")
				}
				_, port, _ := net.SplitHostPort(address)
				return d.DialContext(r.ctx, network, net.JoinHostPort(loopback, port))
			}
			t.Run("TCP half close", func(t *testing.T) {
				ln, err := net.Listen("tcp"+family, net.JoinHostPort(loopback, "0"))
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				go func() {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					b, _ := io.ReadAll(c)
					_, _ = c.Write(append(b, []byte(" reply")...))
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				c, err := client.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(remote, uint16(ln.Addr().(*net.TCPAddr).Port)))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = c.Write(bytes.Repeat([]byte("x"), 10000))
				_ = c.CloseWrite()
				b, err := io.ReadAll(c)
				if err != nil || !bytes.Equal(b, append(bytes.Repeat([]byte("x"), 10000), []byte(" reply")...)) {
					t.Fatalf("TCP half close: %d bytes, %v", len(b), err)
				}
			})
			t.Run("UDP same destination multiple sources", func(t *testing.T) {
				ln, err := net.ListenPacket("udp"+family, net.JoinHostPort(loopback, "0"))
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
				var connections []net.Conn
				for j := 0; j < 3; j++ {
					c, err := client.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(remote, uint16(ln.LocalAddr().(*net.UDPAddr).Port)))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					connections = append(connections, c)
				}
				for _, size := range []int{0, 10, 5000, 20000} {
					for _, c := range connections {
						_ = c.SetDeadline(time.Now().Add(5 * time.Second))
						want := bytes.Repeat([]byte{byte(size)}, size)
						if _, err := c.Write(want); err != nil {
							t.Fatal(err)
						}
						b := make([]byte, 65535)
						n, err := c.Read(b)
						if err != nil || !bytes.Equal(b[:n], want) {
							t.Fatalf("UDP %d-byte datagram: %d bytes, %v", size, n, err)
						}
					}
				}
			})
			t.Run("revoke closes existing session", func(t *testing.T) {
				ln, err := net.Listen("tcp"+family, net.JoinHostPort(loopback, "0"))
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				accepted := make(chan net.Conn, 1)
				go func() {
					c, err := ln.Accept()
					if err == nil {
						accepted <- c
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				c, err := client.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(remote, uint16(ln.Addr().(*net.TCPAddr).Port)))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				host := <-accepted
				defer host.Close()
				r.SetPeers(nil)
				_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
				b := make([]byte, 1)
				if _, err = host.Read(b); !errors.Is(err, io.EOF) {
					t.Fatalf("revoked upstream connection still alive: %v", err)
				}
			})
		})
	}
}

func TestEchoRequiresRealSuccess(t *testing.T) {
	for _, is6 := range []bool{false, true} {
		r, client, packets := testRouter(t)
		remote := netip.MustParseAddr("203.0.113.8")
		var typ icmp.Type = ipv4.ICMPTypeEcho
		if is6 {
			remote = netip.MustParseAddr("2001:db8::8")
			typ = ipv6.ICMPTypeEchoRequest
		}
		var fail sync.Once
		r.ping = func(ctx context.Context, source, destination netip.Addr, data []byte) ([]byte, error) {
			first := false
			fail.Do(func() { first = true })
			if first {
				return nil, errors.New("destination timed out")
			}
			return append([]byte(nil), data...), nil
		}
		c, err := client.DialPingAddr(netip.Addr{}, remote)
		if err != nil {
			t.Fatal(err)
		}
		packet, _ := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: 1, Seq: 2, Data: []byte("probe")}}).Marshal(nil)
		_, _ = c.Write(packet)
		select {
		case <-packets:
			t.Fatal("unreachable destination produced a reply")
		case <-time.After(100 * time.Millisecond):
		}
		_, _ = c.Write(packet)
		select {
		case reply := <-packets:
			info, ok := inspectPacket(reply)
			if !ok || info.src != remote || len(reply) < info.offset+8 || !bytes.Equal(reply[info.offset+8:], []byte("probe")) {
				t.Fatalf("invalid real echo reply: %x", reply)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("real echo success produced no reply")
		}
		_ = c.Close()
	}
}

func TestMSSAndMalformedPackets(t *testing.T) {
	for _, family := range []int{4, 6} {
		src, dst := netip.MustParseAddr("10.20.0.2"), netip.MustParseAddr("10.20.0.3")
		if family == 6 {
			src, dst = netip.MustParseAddr("fd20::2"), netip.MustParseAddr("fd20::3")
		}
		tcp := make([]byte, 24)
		tcp[12], tcp[13] = 0x60, 2
		copy(tcp[20:], []byte{2, 4, 0x5b, 0x4})
		binary.BigEndian.PutUint16(tcp[16:], transportChecksum(src, dst, 6, tcp))
		p := ipPacket(src, dst, 6, tcp)
		info, ok := inspectPacket(p)
		if !ok {
			t.Fatal("valid SYN rejected")
		}
		clampMSS(p, info, 1380)
		want := 1380 - info.offset - 20
		if got := int(binary.BigEndian.Uint16(p[info.offset+22:])); got != want {
			t.Fatalf("MSS = %d, want %d", got, want)
		}
		if transportChecksum(src, dst, 6, p[info.offset:]) != 0 {
			t.Fatal("MSS rewrite corrupted TCP checksum")
		}
		for size := 0; size < len(p); size++ {
			if _, ok := inspectPacket(p[:size]); ok {
				t.Fatalf("truncated %d-byte packet accepted", size)
			}
		}
	}
}
