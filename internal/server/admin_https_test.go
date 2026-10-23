package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/config"
	"github.com/lehuunghi/vpn/internal/ddns"
)

type subdomainState struct{}

func (subdomainState) GetSetting(context.Context, string) (string, error) {
	return `{"active":"office.vpn.20.com.vn","keys":{"office.vpn.20.com.vn":"private-key"}}`, nil
}
func (subdomainState) SetSetting(context.Context, string, string) error { return nil }

func adminTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "office.vpn.20.com.vn"}, DNSNames: []string{"office.vpn.20.com.vn"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return pair, roots
}

func TestOneAdminSocketServesHTTPAndTrustedHTTPSWithoutBlocking(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pair, roots := adminTestCertificate(t)
	m := ddns.New(ddns.Options{AdminAddress: listener.Addr().String(), Store: subdomainState{}})
	s := &Server{cfg: &config.Config{}, ddns: m}
	app := s.automaticHTTPS(s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setCookie(w, r, "test-session", time.Now().Add(time.Hour))
		if r.TLS != nil {
			io.WriteString(w, "encrypted")
		} else {
			io.WriteString(w, "lan")
		}
	})))
	httpServer := &http.Server{Handler: app, ReadHeaderTimeout: 2 * time.Second, ConnContext: func(ctx context.Context, c net.Conn) context.Context { return context.WithValue(ctx, protocolKey{}, c) }}
	defer httpServer.Close()
	done := make(chan error, 1)
	go func() {
		done <- httpServer.Serve(&adminListener{Listener: listener, ctx: context.Background(), tls: &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}})
	}()
	t.Cleanup(func() { _ = httpServer.Close(); <-done })
	// An idle TCP connection must not hold up listener.Accept for everyone.
	idle, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	plain := &http.Client{Timeout: 2 * time.Second}
	res, err := plain.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(data) != "lan" || len(res.Cookies()) != 1 || res.Cookies()[0].Secure {
		t.Fatal(string(data), res.Header)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	}}}
	defer client.CloseIdleConnections()
	url := "https://office.vpn.20.com.vn:" + port
	res, err = client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if string(data) != "encrypted" || len(res.Cookies()) != 1 || !res.Cookies()[0].Secure || res.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal(string(data), res.Header)
	}
	request, _ := http.NewRequest("GET", url, nil)
	request.Host = "other.vpn.20.com.vn"
	res, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 421 {
		t.Fatal(res.Status)
	}
}

func TestPublicHTTPAndHostnameRedirectToHTTPSOnSameAdminPort(t *testing.T) {
	m := ddns.New(ddns.Options{AdminAddress: "0.0.0.0:51821", Store: subdomainState{}})
	s := &Server{ddns: m}
	handler := s.automaticHTTPS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		host, peer string
		status     int
	}{
		{"192.168.1.100:51821", "192.168.1.2:1234", 204},
		{"127.0.0.1:51821", "127.0.0.1:1234", 204},
		{"office.vpn.20.com.vn:51821", "192.168.1.2:1234", 307},
		{"192.168.1.100:51821", "8.8.8.8:1234", 307},
	} {
		request := httptest.NewRequest("GET", "http://"+tc.host+"/api/status?check=1", nil)
		request.RemoteAddr = tc.peer
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tc.status {
			t.Fatal(tc, recorder.Code)
		}
		if tc.status == 307 && recorder.Header().Get("Location") != "https://office.vpn.20.com.vn:51821/api/status?check=1" {
			t.Fatal(recorder.Header())
		}
		if strings.Contains(recorder.Body.String(), "private-key") {
			t.Fatal("key leaked")
		}
	}
}
