package vpsrelay

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu    sync.Mutex
	value string
	fail  bool
}

func (s *memoryStore) GetSetting(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, nil
}
func (s *memoryStore) SetSetting(_ context.Context, _ string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("private database error")
	}
	s.value = value
	return nil
}

func fixtureCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"relay.example.test"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	cert, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func profileConfig(control, remote int) string {
	return fmt.Sprintf(`serverAddr = "relay.example.test"
serverPort = %d
user = "office"
loginFailExit = false
metadatas.access_key = "%s"
auth.method = "token"
auth.token = "%s"
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
transport.tls.enable = true
transport.tls.serverName = "relay.example.test"
transport.tls.trustedCaFile = "ca.pem"
transport.heartbeatInterval = 10
transport.heartbeatTimeout = 30
[[proxies]]
name = "vpn20"
type = "udp"
localIP = "192.0.2.100"
localPort = 9
remotePort = %d
transport.bandwidthLimit = "1MB"
transport.bandwidthLimitMode = "server"
`, control, strings.Repeat("a", 64), strings.Repeat("b", 64), remote)
}
func zipProfile(t *testing.T, config string, ca []byte, extra map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	all := map[string][]byte{"frpc.toml": []byte(config), "ca.pem": ca}
	for name, body := range extra {
		all[name] = body
	}
	for name, body := range all {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProfileValidationAndNoScriptExecution(t *testing.T) {
	ca, _ := fixtureCertificate(t)
	cfg := profileConfig(443, 20001)
	p, err := parseProfile(zipProfile(t, cfg, ca, map[string][]byte{"install-client.sh": []byte("exit 99")}))
	if err != nil || p.RemotePort != 20001 || p.Name != "office" {
		t.Fatalf("parse: %v", err)
	}
	for _, change := range [][2]string{
		{`type = "udp"`, `type = "tcp"`}, {`transport.tls.enable = true`, `transport.tls.enable = false`},
		{`trustedCaFile = "ca.pem"`, `trustedCaFile = "/etc/passwd"`},
		{`serverName = "relay.example.test"`, `serverName = "other.example.test"`},
		{strings.Repeat("a", 64), "bad"}, {`user = "office"`, `user = "../escape"`},
	} {
		if _, err := parseProfile(zipProfile(t, strings.ReplaceAll(cfg, change[0], change[1]), ca, nil)); err == nil {
			t.Fatalf("accepted invalid profile field: %s", change[0])
		}
	}
	if _, err := parseProfile(zipProfile(t, cfg, ca, map[string][]byte{"../escape": []byte("x")})); err == nil {
		t.Fatal("accepted traversal")
	}
	if _, err := parseProfile(zipProfile(t, cfg, []byte("not a CA"), nil)); err == nil {
		t.Fatal("accepted invalid CA")
	}
	if _, err := parseProfile(zipProfile(t, cfg, bytes.Repeat([]byte("x"), MaxProfileSize+1), nil)); err == nil {
		t.Fatal("accepted compressed oversized CA")
	}
	if _, err := parseProfile(make([]byte, MaxProfileSize+1)); err == nil {
		t.Fatal("accepted oversized ZIP")
	}
	for _, host := range []string{"https://relay.example.test", "relay.example.test:443", "*.example.test", "127.0.0.1", "a\nb.test", "a..test"} {
		if _, err := Domain(host); err == nil {
			t.Fatalf("accepted domain %q", host)
		}
	}
}

func TestPersistenceCredentialsAndFailedSave(t *testing.T) {
	ca, _ := fixtureCertificate(t)
	st := &memoryStore{}
	opts := Options{Store: st, LocalPort: 51820}
	m := New(opts)
	s, err := m.Configure(context.Background(), Input{Enabled: true, ProfileZIP: zipProfile(t, profileConfig(443, 20001), ca, nil)})
	if err != nil || s.Domain != "relay.example.test" || s.Phase != "connecting" {
		t.Fatalf("configure: %+v %v", s, err)
	}
	jsonStatus, _ := json.Marshal(s)
	if strings.Contains(string(jsonStatus), strings.Repeat("a", 64)) || strings.Contains(string(jsonStatus), strings.Repeat("b", 64)) ||
		strings.Contains(string(jsonStatus), "BEGIN CERTIFICATE") {
		t.Fatal("credentials disclosed")
	}
	st.fail = true
	if _, err := m.Configure(context.Background(), Input{Enabled: false}); err == nil {
		t.Fatal("accepted failed persistence")
	}
	if _, _, enabled := m.Endpoint(); !enabled {
		t.Fatal("failed save changed active selection")
	}
	st.fail = false
	m2 := New(opts)
	if host, port, on := m2.Endpoint(); host != "relay.example.test" || port != 20001 || !on {
		t.Fatal("restart lost relay")
	}
	if _, err := m2.Configure(context.Background(), Input{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, _, on := m2.Endpoint(); on {
		t.Fatal("disable left relay selected")
	}
	if !m2.Snapshot().Configured {
		t.Fatal("disable discarded profile")
	}
	if _, err := m2.Configure(context.Background(), Input{Enabled: true, Domain: "new.example.test"}); err != nil {
		t.Fatal(err)
	}
	if New(opts).Snapshot().Domain != "new.example.test" {
		t.Fatal("custom domain not retained")
	}
}
