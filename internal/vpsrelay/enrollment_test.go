package vpsrelay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type enrollmentStore struct {
	mu        sync.Mutex
	values    map[string]string
	failState bool
}

func (s *enrollmentStore) GetSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}
func (s *enrollmentStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failState && key == stateKey {
		return errors.New("private storage failure")
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func TestPasswordEnrollmentTLSRetryPersistenceAndPrivacy(t *testing.T) {
	ca, key := fixtureCertificate(t)
	cert, err := tls.X509KeyPair(ca, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	st := &enrollmentStore{failState: true}
	var requests atomic.Int32
	var claim string
	var handlerMu sync.Mutex
	var mode atomic.Int32
	var p profile
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		requests.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/enroll" || r.TLS.NegotiatedProtocol != "http/1.1" {
			t.Error("wrong enrollment protocol")
		}
		var input map[string]string
		if json.NewDecoder(r.Body).Decode(&input) != nil || input["password"] != "private-enrollment-password" {
			t.Error("wrong enrollment credentials")
		}
		if !keyPattern.MatchString(input["claim"]) {
			t.Error("invalid enrollment claim")
		}
		if claim != "" && claim != input["claim"] {
			t.Error("retry changed registration claim")
		}
		claim = input["claim"]
		if mode.Load() == 1 {
			w.Header().Set("Location", "https://other.example.test/steal")
			w.WriteHeader(302)
			return
		}
		if mode.Load() == 2 {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, "private-enrollment-password")
			return
		}
		if mode.Load() == 3 {
			w.WriteHeader(429)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}
	server.StartTLS()
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	p = profile{Name: "vpn20-fixture", Host: "relay.example.test", ControlPort: port, RemotePort: 20001,
		AccessKey: strings.Repeat("a", 64), Token: strings.Repeat("b", 64)}
	opts := Options{Store: st, LocalPort: 51820, enrollRoots: roots, dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, server.Listener.Addr().String())
	}}
	m := New(opts)
	input := Input{Enabled: true, Domain: fmt.Sprintf("relay.example.test:%d", port), Password: "private-enrollment-password"}
	if _, err = m.Configure(t.Context(), input); err == nil {
		t.Fatal("accepted failed persistence")
	}
	if _, _, on := m.Endpoint(); on {
		t.Fatal("failed persistence enabled relay")
	}
	st.failState = false
	s, err := m.Configure(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || !s.Enabled || s.Endpoint != "relay.example.test:20001" {
		t.Fatalf("enrollment failed: %+v", s)
	}
	for k, v := range st.values {
		if strings.Contains(v, input.Password) {
			t.Fatalf("password persisted in %s", k)
		}
	}
	status, _ := json.Marshal(s)
	handlerMu.Lock()
	savedClaim := claim
	handlerMu.Unlock()
	for _, secret := range []string{input.Password, p.AccessKey, p.Token, savedClaim} {
		if strings.Contains(string(status), secret) {
			t.Fatal("status disclosed enrollment credentials")
		}
	}
	m = New(opts)
	if host, port, on := m.Endpoint(); !on || host != p.Host || port != p.RemotePort {
		t.Fatal("restart lost assigned endpoint")
	}
	if _, err = m.Configure(t.Context(), Input{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Configure(t.Context(), Input{Enabled: true, Domain: p.Host}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("restart/disable/re-enable required password enrollment")
	}
	if _, err = m.Configure(t.Context(), Input{Enabled: true, Domain: "other.example.test"}); err == nil {
		t.Fatal("changed server without enrollment")
	}
	for _, n := range []int32{1, 2, 3} {
		mode.Store(n)
		_, err = m.Configure(t.Context(), input)
		if err == nil || strings.Contains(err.Error(), input.Password) {
			t.Fatal("unsafe enrollment error")
		}
	}
	mode.Store(0)
	before := requests.Load()
	untrusted := opts
	untrusted.enrollRoots = nil
	if _, err = New(untrusted).Configure(t.Context(), input); err == nil || !strings.Contains(err.Error(), "SSL") {
		t.Fatal("accepted an untrusted TLS certificate")
	}
	if requests.Load() != before {
		t.Fatal("sent password before validating TLS")
	}
	handlerMu.Lock()
	p.RemotePort = 0
	handlerMu.Unlock()
	if _, err = m.Configure(t.Context(), input); err == nil {
		t.Fatal("accepted invalid assigned port")
	}
	if port, _, on := m.Endpoint(); !on || port != "relay.example.test" {
		t.Fatal("failed enrollment replaced active selection")
	}
}
