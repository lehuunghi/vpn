package ddns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/miekg/dns"
)

// A small local ACME CA exercises the real CertMagic/acmez issuance path:
// account, order, broker TXT, HTTPS DNS checks, challenge, CSR, signed chain,
// persistent storage and cleanup. No public CA or real Cloudflare token is used.
func TestRealACMEIssuanceViaBrokerAndHTTPSDNS(t *testing.T) {
	const hostname = "office.vpn.20.com.vn"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "VPN20 test root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, _ = x509.ParseCertificate(rootDER)
	var mu sync.Mutex
	var txt string
	var present, checked, cleaned, issued bool
	var certificatePEM []byte
	brokerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-machine-key" {
			t.Error("missing private machine key")
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct{ Value string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/acme/present":
			if len(body.Value) != 43 {
				t.Error("invalid challenge value")
			}
			txt = body.Value
			present = true
		case "/v1/acme/cleanup":
			if body.Value != txt {
				t.Error("cleanup did not match challenge")
			}
			txt = ""
			cleaned = true
		default:
			t.Errorf("unexpected broker path %s", r.URL.Path)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer brokerAPI.Close()
	doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		value := txt
		checked = true
		mu.Unlock()
		if value == "" {
			t.Error("DNS queried before broker present")
		}
		dohReply(w, r, &dns.TXT{Hdr: dns.RR_Header{Name: "_acme-challenge." + hostname + ".", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60}, Txt: []string{value}})
	}))
	defer doh.Close()
	var ca *httptest.Server
	valid := false
	ca = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Replay-Nonce", base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano))))
		payload := []byte("{}")
		if r.Method == "POST" {
			var jws struct{ Payload string }
			_ = json.NewDecoder(r.Body).Decode(&jws)
			payload, _ = base64.RawURLEncoding.DecodeString(jws.Payload)
		}
		mu.Lock()
		defer mu.Unlock()
		order := func() map[string]any {
			status := "pending"
			if valid {
				status = "ready"
			}
			if issued {
				status = "valid"
			}
			result := map[string]any{"status": status, "identifiers": []map[string]string{{"type": "dns", "value": hostname}}, "authorizations": []string{ca.URL + "/auth/1"}, "finalize": ca.URL + "/finalize/1"}
			if issued {
				result["certificate"] = ca.URL + "/cert/1"
			}
			return result
		}
		var response any
		switch r.URL.Path {
		case "/directory":
			response = map[string]string{"newNonce": ca.URL + "/nonce", "newAccount": ca.URL + "/account", "newOrder": ca.URL + "/new-order"}
		case "/nonce":
			w.WriteHeader(200)
			return
		case "/account":
			w.Header().Set("Location", ca.URL+"/acct/1")
			w.WriteHeader(201)
			response = map[string]any{"status": "valid", "orders": ca.URL + "/orders"}
		case "/acct/1":
			response = map[string]string{"status": "valid", "orders": ca.URL + "/orders"}
		case "/new-order":
			if strings.Contains(string(payload), "blocked.vpn.20.com.vn") {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:ietf:params:acme:error:caa", "status": 403, "detail": "SECRET CA detail"})
				return
			}
			w.Header().Set("Location", ca.URL+"/order/1")
			w.WriteHeader(201)
			response = order()
		case "/order/1":
			response = order()
		case "/auth/1":
			status := "pending"
			if valid {
				status = "valid"
			}
			response = map[string]any{"identifier": map[string]string{"type": "dns", "value": hostname}, "status": status, "challenges": []map[string]string{{"type": "dns-01", "url": ca.URL + "/challenge/1", "token": "local-token", "status": status}}}
		case "/challenge/1":
			if !present || !checked || txt == "" {
				t.Error("CA challenge started without a visible TXT")
				http.Error(w, "not ready", 500)
				return
			}
			valid = true
			response = map[string]string{"type": "dns-01", "url": ca.URL + "/challenge/1", "token": "local-token", "status": "valid"}
		case "/finalize/1":
			var body struct{ CSR string }
			_ = json.Unmarshal(payload, &body)
			csrDER, _ := base64.RawURLEncoding.DecodeString(body.CSR)
			csr, e := x509.ParseCertificateRequest(csrDER)
			if e != nil || csr.CheckSignature() != nil || !valid || len(csr.DNSNames) != 1 || csr.DNSNames[0] != hostname {
				t.Error("invalid CSR/order")
				http.Error(w, "bad csr", 400)
				return
			}
			leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: csr.Subject, DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, e := x509.CreateCertificate(rand.Reader, leaf, root, csr.PublicKey, key)
			if e != nil {
				t.Error(e)
				http.Error(w, "signing failed", 500)
				return
			}
			certificatePEM = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})...)
			issued = true
			response = order()
		case "/cert/1":
			w.Header().Set("Content-Type", "application/pem-certificate-chain")
			_, _ = w.Write(certificatePEM)
			return
		default:
			t.Errorf("unexpected ACME path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer ca.Close()
	m := New(Options{APIURL: brokerAPI.URL, ACMECA: ca.URL + "/directory", DataDir: t.TempDir()})
	defer m.broker.close()
	m.state.Keys[hostname] = "private-machine-key"
	m.state.Active = hostname
	manager, err := newCertificates(m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if manager != nil {
			manager.Close()
		}
	}()
	automatic := manager.(*automaticCertificates)
	solver := automatic.config.Issuers[0].(*certmagic.ACMEIssuer).DNS01Solver.(*brokerSolver)
	solver.dns = &txtChecker{client: doh.Client(), endpoints: []string{doh.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := automatic.Manage(ctx, hostname); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	complete := present && checked && cleaned && issued && txt == ""
	mu.Unlock()
	if !complete || automatic.Expires(hostname).IsZero() {
		t.Fatal("ACME lifecycle not complete")
	}
	m.certs = automatic
	certificate, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: hostname})
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: hostname, Roots: roots}); err != nil {
		t.Fatal(err)
	}
	// Restart reads the persisted certificate without a new order/claim.
	automatic.Close()
	manager = nil
	manager2, err := newCertificates(m)
	if err != nil {
		t.Fatal(err)
	}
	defer manager2.Close()
	if err := manager2.Manage(ctx, hostname); err != nil || manager2.Expires(hostname).IsZero() {
		t.Fatal("certificate not restored", err)
	}
	if strings.Contains(certificateMessage(&sslError{"dns_txt_not_visible", "TXT missing", nil}), "private-machine-key") {
		t.Fatal("secret leaked")
	}
	// A real ACME problem survives CertMagic's wrappers and its retry driver.
	started := time.Now()
	err = manager2.Manage(ctx, "blocked.vpn.20.com.vn")
	if err == nil || !strings.Contains(certificateMessage(err), "acme_caa") || strings.Contains(certificateMessage(err), "SECRET") || time.Since(started) > 2*time.Second {
		t.Fatal("ACME error lost or retried internally", err)
	}
}
