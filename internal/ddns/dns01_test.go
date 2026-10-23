package ddns

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mholt/acmez/v3/acme"
	"github.com/miekg/dns"
)

func testChallenge() acme.Challenge {
	return acme.Challenge{Type: "dns-01", KeyAuthorization: "token.account-thumbprint", Identifier: acme.Identifier{Type: "dns", Value: "office." + Domain}}
}

func TestBrokerSolverLimitsOwnershipAndCleansUpAfterCancellation(t *testing.T) {
	var calls atomic.Int32
	ch := testChallenge()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input struct{ Value string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input.Value != ch.DNS01KeyAuthorization() || r.Header.Get("Authorization") != "Bearer private-key" || r.URL.Path != "/v1/acme/present" && r.URL.Path != "/v1/acme/cleanup" {
			t.Errorf("wrong broker request: %s", r.URL.Path)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer api.Close()
	m := New(Options{APIURL: api.URL})
	defer m.broker.close()
	m.state.Keys[ch.Identifier.Value] = "private-key"
	s := newBrokerSolver(m)
	if err := s.Present(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.CleanUp(ctx, ch); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []acme.Challenge{
		{Type: "http-01", Identifier: ch.Identifier, KeyAuthorization: ch.KeyAuthorization},
		{Type: "dns-01", Identifier: acme.Identifier{Type: "dns", Value: "other." + Domain}, KeyAuthorization: ch.KeyAuthorization},
		{Type: "dns-01", Identifier: acme.Identifier{Type: "dns", Value: "*." + Domain}, KeyAuthorization: ch.KeyAuthorization},
		{Type: "dns-01", Identifier: acme.Identifier{Type: "ip", Value: "8.8.8.8"}, KeyAuthorization: ch.KeyAuthorization},
	} {
		if err := s.Present(context.Background(), bad); err == nil {
			t.Fatal("accepted foreign/invalid challenge")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("out-of-scope call reached broker")
	}
}

func dohReply(w http.ResponseWriter, r *http.Request, answers ...dns.RR) {
	query, _ := io.ReadAll(r.Body)
	var request dns.Msg
	_ = request.Unpack(query)
	response := new(dns.Msg).SetReply(&request)
	response.Answer = answers
	wire, _ := response.Pack()
	w.Header().Set("Content-Type", "application/dns-message")
	_, _ = w.Write(wire)
}

func TestTXTCheckerUsesHTTPSAndExactNameAndValue(t *testing.T) {
	ch := testChallenge()
	name, value := ch.DNS01TXTRecordName(), ch.DNS01KeyAuthorization()
	for _, test := range []struct {
		name            string
		rrName, content string
		ready           bool
	}{
		{"visible", name, value, true}, {"wrong name", "_acme-challenge.other." + Domain, value, false}, {"wrong value", name, "different", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/dns-message" || r.Header.Get("Authorization") != "" {
					t.Error("unexpected DoH request")
				}
				data := test.content
				dohReply(w, r, &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(test.rrName), Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 60}, Txt: []string{data[:2], data[2:]}})
			}))
			defer server.Close()
			checker := &txtChecker{client: server.Client(), endpoints: []string{server.URL}}
			ready, err := checker.visible(context.Background(), name, value)
			if ready != test.ready || err != nil {
				t.Fatal(ready, err)
			}
		})
	}
}

func TestTXTCheckerFallsBackAndRejectsHTMLOrMismatchedDNS(t *testing.T) {
	ch := testChallenge()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			io.WriteString(w, "SECRET HTML")
		case "/mismatch":
			query, _ := io.ReadAll(r.Body)
			var request dns.Msg
			_ = request.Unpack(query)
			response := new(dns.Msg).SetReply(&request)
			response.Question[0].Name = "foreign.example."
			wire, _ := response.Pack()
			w.Header().Set("Content-Type", "application/dns-message")
			_, _ = w.Write(wire)
		default:
			dohReply(w, r, &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(ch.DNS01TXTRecordName()), Rrtype: dns.TypeTXT, Class: dns.ClassINET}, Txt: []string{ch.DNS01KeyAuthorization()}})
		}
	}))
	defer server.Close()
	for _, path := range []string{"/html", "/mismatch"} {
		checker := &txtChecker{client: server.Client(), endpoints: []string{server.URL + path}}
		ready, err := checker.visible(context.Background(), ch.DNS01TXTRecordName(), ch.DNS01KeyAuthorization())
		if ready || err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(ready, err)
		}
		checker.endpoints = append(checker.endpoints, server.URL+"/valid")
		if ready, err = checker.visible(context.Background(), ch.DNS01TXTRecordName(), ch.DNS01KeyAuthorization()); !ready || err != nil {
			t.Fatal(ready, err)
		}
	}
}

func TestBrokerSolverWaitsForPublicTXTAndHonorsCancellation(t *testing.T) {
	ch := testChallenge()
	var lookups atomic.Int32
	var show atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lookups.Add(1)
		if !show.Load() {
			dohReply(w, r)
			return
		}
		dohReply(w, r, &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(ch.DNS01TXTRecordName()), Rrtype: dns.TypeTXT, Class: dns.ClassINET}, Txt: []string{ch.DNS01KeyAuthorization()}})
	}))
	defer server.Close()
	m := New(Options{})
	defer m.broker.close()
	m.state.Keys[ch.Identifier.Value] = "key"
	s := newBrokerSolver(m)
	s.timeout = 200 * time.Millisecond
	s.interval = time.Millisecond
	s.dns = &txtChecker{client: server.Client(), endpoints: []string{server.URL}}
	err := s.Wait(context.Background(), ch)
	if err == nil || !strings.Contains(err.Error(), "dns_txt_not_visible") || lookups.Load() == 0 {
		t.Fatal(err)
	}
	show.Store(true)
	if err := s.Wait(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Wait(ctx, ch); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCertificateErrorsRemainActionableWithoutUntrustedDetail(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{acme.Problem{Type: "urn:ietf:params:acme:error:caa", Detail: "SECRET"}, "acme_caa"},
		{acme.Problem{Type: "urn:ietf:params:acme:error:rateLimited", Detail: "SECRET"}, "acme_rateLimited"},
		{acme.Problem{Type: "urn:ietf:params:acme:error:dns", Detail: "SECRET"}, "acme_dns"},
		{&sslError{"dns_txt_not_visible", "TXT not visible", errors.New("SECRET")}, "dns_txt_not_visible"},
		{&apiError{503, "schema_outdated", "D1 needs schema.sql"}, "schema.sql"},
		{errors.New("SECRET"), "certificate_failed"},
	} {
		message := certificateMessage(fmt.Errorf("issuer wrapper: %w", test.err))
		if !strings.Contains(message, test.code) || strings.Contains(message, "SECRET") {
			t.Fatal(message)
		}
	}
}

func TestCertificateFailureSurvivesAutomaticBackoff(t *testing.T) {
	api, _ := testBroker(t)
	certs := &testCertificates{certs: map[string]*tls.Certificate{}}
	certs.fail.Store(true)
	var active atomic.Value
	m := startManager(t, api.URL, &memoryStore{}, certs, &active)
	_, _ = m.Configure(context.Background(), "office")
	before := await(t, m, "error")
	m.reconcile(context.Background())
	after := await(t, m, "error")
	if !strings.Contains(before.Message, "certificate_failed") || !strings.Contains(after.Message, "certificate_failed") {
		t.Fatal(before, after)
	}
}
