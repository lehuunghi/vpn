//go:build windows

package relay

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestWindowsHostEchoAPI(t *testing.T) {
	for _, destination := range []string{"127.0.0.1", "::1"} {
		t.Run(destination, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			payload, err := pingHost(ctx, netip.Addr{}, netip.MustParseAddr(destination), []byte("VPN20 IP Helper echo"))
			if err != nil || string(payload) != "VPN20 IP Helper echo" {
				t.Fatalf("real Windows echo API: %q %v", payload, err)
			}
		})
	}
}
