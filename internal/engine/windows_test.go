//go:build windows

package engine

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsDefaultsForPeerRouting(t *testing.T) {
	e, _ := newTestEngine(t)
	s := e.Settings()
	want := e.cfg.Subnet4.Masked().String() + ", " + e.cfg.Subnet6.Masked().String()
	if s.PeerIsolation || s.DNS != "" || s.ClientRoutes != want {
		t.Fatalf("unsafe peer-routing defaults: %+v", s)
	}
	p, err := e.CreatePeer(context.Background(), PeerInput{Name: "Windows peer"})
	if err != nil {
		t.Fatal(err)
	}
	conf := e.ClientConfig(p)
	if strings.Contains(conf, "0.0.0.0/0") || strings.Contains(conf, "DNS =") || !strings.Contains(conf, "AllowedIPs = "+s.ClientRoutes) {
		t.Fatal("exported configuration does not preserve peer routes and local DNS")
	}
}
