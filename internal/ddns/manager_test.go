package ddns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libdns/libdns"
)

type memoryStore struct {
	mu   sync.Mutex
	data string
}

func (s *memoryStore) GetSetting(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, nil
}
func (s *memoryStore) SetSetting(_ context.Context, _ string, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = v
	return nil
}

type testCertificates struct {
	mu    sync.Mutex
	certs map[string]*tls.Certificate
	fail  atomic.Bool
}

func (c *testCertificates) Manage(_ context.Context, hostname string) error {
	if c.fail.Load() {
		return errors.New("test CA unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.certs[hostname] != nil {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: hostname},
		DNSNames: []string{hostname}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		return err
	}
	pair.Leaf, _ = x509.ParseCertificate(der)
	c.certs[hostname] = &pair
	return nil
}
func (c *testCertificates) GetCertificate(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cert := c.certs[h.ServerName]
	if cert == nil {
		return nil, errors.New("no certificate")
	}
	return cert, nil
}
func (c *testCertificates) Expires(h string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cert := c.certs[h]; cert != nil {
		return cert.Leaf.NotAfter
	}
	return time.Time{}
}
func (c *testCertificates) Forget(h string) { c.mu.Lock(); delete(c.certs, h); c.mu.Unlock() }
func (c *testCertificates) Close()          {}

type brokerFixture struct {
	mu        sync.Mutex
	keys      map[string]string
	claims    []string
	retired   []string
	updates   int
	failClaim atomic.Bool
}

func testBroker(t *testing.T) (*httptest.Server, *brokerFixture) {
	t.Helper()
	f := &brokerFixture{keys: map[string]string{}}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/ip":
			io.WriteString(w, `{"ok":true,"ip":"8.8.8.8","type":"A"}`)
		case "/v1/claim":
			var in struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			f.mu.Lock()
			f.claims = append(f.claims, key)
			prior := f.keys[in.Name]
			if prior == "" {
				f.keys[in.Name] = key
			}
			f.mu.Unlock()
			if f.failClaim.Load() {
				w.WriteHeader(503)
				io.WriteString(w, `{"error":"service_error"}`)
				return
			}
			if prior != "" && prior != key {
				w.WriteHeader(409)
				io.WriteString(w, `{"error":"name_taken"}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "hostname": in.Name + "." + Domain})
		case "/v1/update":
			f.mu.Lock()
			f.updates++
			f.mu.Unlock()
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["ipv4"] != "8.8.8.8" || in["ipv6"] != nil {
				t.Errorf("unexpected dual-stack payload: %v", in)
			}
			io.WriteString(w, `{"ok":true}`)
		case "/v1/retire":
			f.mu.Lock()
			f.retired = append(f.retired, r.Header.Get("Authorization"))
			f.mu.Unlock()
			io.WriteString(w, `{"ok":true}`)
		case "/v1/acme/present", "/v1/acme/cleanup":
			io.WriteString(w, `{"ok":true}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"not_found"}`)
		}
	}))
	t.Cleanup(api.Close)
	return api, f
}
func startManager(t *testing.T, api string, store *memoryStore, certs *testCertificates, activated *atomic.Value) *Manager {
	t.Helper()
	m := New(Options{APIURL: api, HTTPSListen: "127.0.0.1:0", DataDir: t.TempDir(), Store: store, interval: time.Hour,
		Handler:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure") }),
		Activate: func(_ context.Context, h string) error { activated.Store(h); return nil },
		factory:  func(*Manager) (certificates, error) { return certs, nil }})
	m.Start(context.Background())
	t.Cleanup(m.Close)
	return m
}
func await(t *testing.T, m *Manager, phase string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if !s.Busy && s.Phase == phase {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("did not reach %s: %+v", phase, m.Snapshot())
	return Status{}
}
func TestProvisionAndRealHTTPS(t *testing.T) {
	api, f := testBroker(t)
	store := &memoryStore{}
	certs := &testCertificates{certs: map[string]*tls.Certificate{}}
	var active atomic.Value
	m := startManager(t, api.URL, store, certs, &active)
	if _, err := m.Configure(context.Background(), "Office"); err != nil {
		t.Fatal(err)
	}
	status := await(t, m, "ready")
	if !status.SSLReady || status.Hostname != "office."+Domain || active.Load() != status.Hostname || status.Reachability != "not_checked" {
		t.Fatalf("%+v", status)
	}
	f.mu.Lock()
	secret := f.keys["office"]
	f.mu.Unlock()
	data, _ := json.Marshal(status)
	if strings.Contains(string(data), secret) || len(secret) != 64 {
		t.Fatal("key leaked or invalid")
	}
	roots := x509.NewCertPool()
	certs.mu.Lock()
	roots.AddCert(certs.certs[status.Hostname].Leaf)
	certs.mu.Unlock()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", m.address)
		},
	}}
	defer client.CloseIdleConnections()
	res, err := client.Get(status.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "secure" {
		t.Fatal(string(body))
	}
	request, _ := http.NewRequest("GET", status.URL, nil)
	request.Host = "other." + Domain
	res2, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 421 {
		t.Fatal(res2.Status)
	}
}
func TestRenameFailureKeepsActiveAndRetries(t *testing.T) {
	api, f := testBroker(t)
	store := &memoryStore{}
	certs := &testCertificates{certs: map[string]*tls.Certificate{}}
	var active atomic.Value
	m := startManager(t, api.URL, store, certs, &active)
	_, _ = m.Configure(context.Background(), "office")
	await(t, m, "ready")
	certs.fail.Store(true)
	_, err := m.Configure(context.Background(), "new-office")
	if err != nil {
		t.Fatal(err)
	}
	status := await(t, m, "error")
	if status.Hostname != "office."+Domain || !status.SSLReady || active.Load() != status.Hostname {
		t.Fatalf("old name lost: %+v", status)
	}
	certs.fail.Store(false)
	_, err = m.Configure(context.Background(), "new-office")
	if err != nil {
		t.Fatal(err)
	}
	status = await(t, m, "ready")
	if status.Hostname != "new-office."+Domain || active.Load() != status.Hostname {
		t.Fatal(status)
	}
	f.mu.Lock()
	retired := len(f.retired)
	f.mu.Unlock()
	if retired != 1 {
		t.Fatal("old DNS not retired")
	}
}
func TestLostClaimResponseSurvivesRestartWithSameKey(t *testing.T) {
	api, f := testBroker(t)
	store := &memoryStore{}
	certs := &testCertificates{certs: map[string]*tls.Certificate{}}
	var active atomic.Value
	f.failClaim.Store(true)
	first := startManager(t, api.URL, store, certs, &active)
	_, _ = first.Configure(context.Background(), "office")
	await(t, first, "error")
	first.Close()
	f.failClaim.Store(false)
	second := startManager(t, api.URL, store, certs, &active)
	await(t, second, "ready")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.claims) != 2 || f.claims[0] != f.claims[1] {
		t.Fatalf("lost key after restart: %v", f.claims)
	}
}
func TestDNSProviderRejectsOtherNamesAndTypes(t *testing.T) {
	api, _ := testBroker(t)
	store := &memoryStore{}
	certs := &testCertificates{certs: map[string]*tls.Certificate{}}
	var active atomic.Value
	m := startManager(t, api.URL, store, certs, &active)
	_, _ = m.Configure(context.Background(), "office")
	await(t, m, "ready")
	p := &dnsProvider{manager: m}
	good := libdns.TXT{Name: "_acme-challenge.office.vpn", Text: strings.Repeat("a", 43)}
	if _, err := p.AppendRecords(context.Background(), "20.com.vn.", []libdns.Record{good}); err != nil {
		t.Fatal(err)
	}
	for _, record := range []libdns.Record{
		libdns.TXT{Name: "_acme-challenge.other.vpn", Text: strings.Repeat("a", 43)},
		libdns.TXT{Name: "office.vpn", Text: strings.Repeat("a", 43)},
		libdns.RR{Name: "_acme-challenge.office.vpn", Type: "A", Data: "8.8.8.8"},
	} {
		if _, err := p.AppendRecords(context.Background(), "20.com.vn.", []libdns.Record{record}); err == nil {
			t.Fatal("allowed out-of-scope record")
		}
	}
	if _, err := p.DeleteRecords(context.Background(), "20.com.vn.", []libdns.Record{good}); err != nil {
		t.Fatal(err)
	}
}
func TestOccupiedHTTPSPortDoesNotClaimName(t *testing.T) {
	api, f := testBroker(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	m := New(Options{APIURL: api.URL, HTTPSListen: ln.Addr().String(), Store: &memoryStore{}, DataDir: t.TempDir()})
	m.Start(context.Background())
	defer m.Close()
	_, _ = m.Configure(context.Background(), "office")
	status := await(t, m, "error")
	if !strings.Contains(status.Message, "cổng HTTPS") {
		t.Fatal(status)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.claims) != 0 {
		t.Fatal("name claimed despite port collision")
	}
}
func TestLabelAndHTTPSDefaults(t *testing.T) {
	if DefaultHTTPS != ":443" {
		t.Fatal("default must be port 443")
	}
	for _, value := range []string{"", "-bad", "bad-", "a.b", "office.vpn.20.com.vn", "admin", strings.Repeat("a", 64)} {
		if _, err := Label(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if name, err := Label(" Office-01 "); err != nil || name != "office-01" {
		t.Fatal(name, err)
	}
	if _, err := newBroker("http://example.com"); err == nil {
		t.Fatal("allowed cleartext public API")
	}
}
func TestBrokerRejectsRedirectAndRedactsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"unknown","message":"SECRET"}`)
	}))
	defer server.Close()
	b, err := newBroker(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	err = b.claim(context.Background(), "office", "test-key")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, 302) }))
	defer redirect.Close()
	b2, _ := newBroker(redirect.URL)
	defer b2.close()
	if err := b2.claim(context.Background(), "office", "test-key"); err == nil {
		t.Fatal("followed redirect")
	}
}
