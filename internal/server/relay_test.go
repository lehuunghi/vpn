package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lehuunghi/vpn/internal/engine"
	"github.com/lehuunghi/vpn/internal/vpsrelay"
)

func relayZIP(t *testing.T) []byte {
	t.Helper()
	srv := httptestCertificate(t)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.Create("ca.pem")
	_, _ = f.Write(srv)
	f, _ = z.Create("frpc.toml")
	_, _ = fmt.Fprintf(f, `serverAddr = "relay.example.test"
serverPort = 443
user = "office"
metadatas.access_key = "%s"
auth.method = "token"
auth.token = "%s"
transport.tls.enable = true
transport.tls.serverName = "relay.example.test"
transport.tls.trustedCaFile = "ca.pem"
[[proxies]]
name = "vpn20"
type = "udp"
remotePort = 20001
`, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func httptestCertificate(t *testing.T) []byte {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer s.Close()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
}

func TestRelayAdminAuthorizationAndEndpointSelection(t *testing.T) {
	c, eng := newClient(t)
	c.expect("GET", "/api/relay", nil, 401)
	c.expect("PUT", "/api/relay", vpsrelay.Input{Enabled: true}, 401)
	c.expect("POST", "/api/setup", map[string]string{"username": "admin", "password": "long-enough-password", "endpointHost": "direct.example.test"}, 201)
	c.expect("PUT", "/api/relay", vpsrelay.Input{Enabled: true, Domain: "relay.example.test"}, 400)
	res, _ := c.do("PUT", "/api/relay", vpsrelay.Input{}, "Sec-Fetch-Site", "cross-site")
	if res.StatusCode != 403 {
		t.Fatal("cross-site update accepted")
	}
	archive := relayZIP(t)
	out := c.expect("PUT", "/api/relay", vpsrelay.Input{Enabled: true, ProfileZIP: archive}, 202)
	var status vpsrelay.Status
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || !status.Configured || status.Endpoint != "relay.example.test:20001" {
		t.Fatal(string(out))
	}
	out = c.expect("GET", "/api/relay", nil, 200)
	for _, secret := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "BEGIN CERTIFICATE", "accessKey", "profileZip"} {
		if strings.Contains(string(out), secret) {
			t.Fatal("status disclosed credentials")
		}
	}
	peer, err := eng.CreatePeer(t.Context(), engine.PeerInput{Name: "relay-device"})
	if err != nil {
		t.Fatal(err)
	}
	before := eng.ClientConfig(peer)
	if !strings.Contains(before, "Endpoint = relay.example.test:20001") {
		t.Fatal("export did not select relay")
	}
	// DDNS updates the direct hostname; selected relay endpoint must survive it.
	if err := eng.SetEndpointHost(t.Context(), "ddns.example.test"); err != nil {
		t.Fatal(err)
	}
	if after := eng.ClientConfig(peer); after != before {
		t.Fatal("DDNS changed relay config, routes or keys")
	}
	c.expect("PUT", "/api/relay", vpsrelay.Input{Enabled: true, Domain: "https://bad.test", ProfileZIP: archive}, 400)
	if _, _, on := eng.Relay().Endpoint(); !on {
		t.Fatal("invalid update disabled relay")
	}
	c.expect("PUT", "/api/relay", vpsrelay.Input{Enabled: false}, 202)
	if !strings.Contains(eng.ClientConfig(peer), "Endpoint = ddns.example.test:51820") {
		t.Fatal("disable did not restore direct endpoint")
	}
	c.expect("POST", "/api/users", map[string]string{"username": "viewer", "password": "long-enough-password", "role": "viewer"}, 201)
	c.expect("POST", "/api/auth/logout", nil, 200)
	c.expect("POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "long-enough-password"}, 200)
	c.expect("GET", "/api/relay", nil, 403)
	c.expect("PUT", "/api/relay", vpsrelay.Input{}, 403)
}
