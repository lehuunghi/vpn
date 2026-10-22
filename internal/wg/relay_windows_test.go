//go:build windows

package wg_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/netcfg"
	"github.com/lehuunghi/vpn/internal/wg"
)

func prepareRelayTest(t *testing.T) {
	t.Helper()
	source := os.Getenv("VPN20_WINTUN_DLL")
	if source == "" {
		t.Skip("set VPN20_WINTUN_DLL to run the real Windows relay test; use packaging/windows/build.ps1")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(exe), "wintun.dll"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN20_DATA_DIR", t.TempDir())
}

func prepareRelayNetwork(t *testing.T, be wg.Backend, port int) {
	t.Helper()
	if err := netcfg.Apply(context.Background(), netcfg.Rules{Relay: true, Iface: "relay-test", ListenPort: port}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = be.Down(context.Background())
		if err := netcfg.Remove(context.Background(), ""); err != nil {
			t.Error(err)
		}
	})
}

func checkRelayHostAccess(t *testing.T, client relayClient) {
	t.Helper()
	// Exercise unsolicited connections into services bound to the VPN address,
	// in addition to connections initiated by the Windows host below.
	for _, ip := range []netip.Addr{netip.MustParseAddr("10.254.249.1"), netip.MustParseAddr("fd20:249::1")} {
		t.Run("VPN device to host TCP "+ip.String(), func(t *testing.T) {
			ln, err := net.ListenTCP("tcp", net.TCPAddrFromAddrPort(netip.AddrPortFrom(ip, 0)))
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := client.net.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(ip, uint16(ln.Addr().(*net.TCPAddr).Port)))
			if err != nil {
				t.Fatalf("VPN device cannot reach a Windows host TCP service: %v", err)
			}
			defer c.Close()
			roundTrip(t, c, []byte("VPN device to Windows host TCP"))
		})
		t.Run("VPN device to host UDP "+ip.String(), func(t *testing.T) {
			ln, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 0)))
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				buf := make([]byte, 1500)
				n, from, err := ln.ReadFromUDP(buf)
				if err == nil {
					_, _ = ln.WriteToUDP(buf[:n], from)
				}
			}()
			c, err := client.net.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(ip, uint16(ln.LocalAddr().(*net.UDPAddr).Port)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			roundTrip(t, c, []byte("VPN device to Windows host UDP"))
		})
	}
	for _, ip := range []netip.Addr{client.ip4, client.ip6} {
		ln, err := client.net.ListenTCPAddrPort(netip.AddrPortFrom(ip, 3388))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
			_, _ = io.Copy(c, c)
		}()
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ip.String(), "3388"), 5*time.Second)
		if err != nil {
			_ = ln.Close()
			t.Fatalf("Windows host cannot initiate a peer connection: %v", err)
		}
		roundTrip(t, c, []byte("Windows host to VPN device"))
		_ = c.Close()
		_ = ln.Close()
		out, err := exec.Command("ping.exe", "-n", "1", "-w", "3000", ip.String()).CombinedOutput()
		if err != nil {
			t.Fatalf("Windows host ping to peer %s: %s %v", ip, out, err)
		}
	}
	// A listener bound only to localhost must remain private over the VPN.
	private, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := client.net.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(netip.MustParseAddr("10.254.249.1"), uint16(private.Addr().(*net.TCPAddr).Port)))
	if err == nil {
		_ = c.Close()
		t.Fatal("VPN device reached a localhost-only service")
	}
}
