package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestWindowsPeerAccessDownload(t *testing.T) {
	c, _ := newClient(t)
	c.expect("GET", "/api/windows/peer-access.ps1", nil, http.StatusUnauthorized)
	c.expect("POST", "/api/setup", map[string]string{"username": "admin", "password": "a-long-enough-password", "endpointHost": "vpn.test"}, http.StatusCreated)
	res, body := c.do("GET", "/api/windows/peer-access.ps1", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Disposition"), "Enable-VPN20-PeerAccess.ps1") {
		t.Fatalf("script download: %d %v", res.StatusCode, res.Header)
	}
	script := string(body)
	if !strings.HasPrefix(script, "\xef\xbb\xbf") || strings.Contains(script, "__VPN20_SUBNET__") || !strings.Contains(script, "[string]$VPNSubnet = '10.8.0.0/24'") {
		t.Fatal("download must be a PowerShell 5.1 compatible script for the configured VPN subnet")
	}
}
