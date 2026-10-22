package ddns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBrokerActionableErrorsWithoutResponseSecrets(t *testing.T) {
	for _, test := range []struct {
		status     int
		code, want string
	}{
		{503, "not_configured", "binding DB"},
		{503, "schema_outdated", "schema.sql"},
		{503, "database_unavailable", "hạn mức D1"},
		{503, "service_error", "0.2.1"},
		{502, "dns_auth_error", "CF_API_TOKEN"},
		{502, "dns_api_unavailable", "API Cloudflare"},
		{302, "unknown-SECRET", "chuyển hướng"},
		{403, "unknown-SECRET", "Cloudflare Access/WAF"},
		{404, "unknown-SECRET", "Deploy Worker"},
		{530, "unknown-SECRET", "HTTP 530"},
	} {
		t.Run(test.code+http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				io.WriteString(w, `{"error":"`+test.code+`","message":"SECRET"}`)
			}))
			defer server.Close()
			b, _ := newBroker(server.URL)
			defer b.close()
			err := b.claim(context.Background(), "office", "SECRET")
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "/v1/claim") || strings.Contains(err.Error(), "SECRET") {
				t.Fatal(err)
			}
		})
	}
}

type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestBrokerClassifiesTransportFailuresWithoutLeakingUnderlyingText(t *testing.T) {
	b, _ := newBroker(DefaultAPI)
	defer b.close()
	for _, test := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "SECRET", Name: "SECRET"}, "Không phân giải"},
		{&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "Chứng chỉ HTTPS"},
		{context.DeadlineExceeded, "thời gian chờ"},
		{context.Canceled, "bị hủy"},
		{errors.New("SECRET"), "Không mở được kết nối"},
	} {
		client := &http.Client{Transport: failTransport{&url.Error{Op: "POST", URL: "https://SECRET", Err: test.err}}}
		err := b.call(context.Background(), client, "POST", "/v1/claim", "SECRET", nil, nil)
		if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "ddns.20.com.vn") || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
}

func TestIPDiscoveryPreservesServiceFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"not_configured","message":"SECRET"}`)
	}))
	defer server.Close()
	b, _ := newBroker(server.URL)
	defer b.close()
	// Both families receive a broker response; neither has a usable source IP.
	b.v6 = b.v4
	_, _, err := b.sync(context.Background(), "SECRET")
	if err == nil || !strings.Contains(err.Error(), "binding DB") || !strings.Contains(err.Error(), "/v1/ip") || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}

func TestHTTPSURLIncludes8443AndPreservesExplicitPort(t *testing.T) {
	for _, test := range []struct{ port, suffix string }{{"8443", ":8443"}, {"443", ""}, {"9443", ":9443"}} {
		m := New(Options{HTTPSListen: ":" + test.port})
		m.state.Active = "office." + Domain
		m.address = "[::]:" + test.port
		if got := m.Snapshot().URL; got != "https://office."+Domain+test.suffix {
			t.Fatal(got)
		}
		if m.opts.HTTPSListen != ":"+test.port {
			t.Fatal("explicit operator port changed")
		}
		m.broker.close()
	}
	if m := New(Options{}); m.opts.HTTPSListen != ":8443" {
		t.Fatal(m.opts.HTTPSListen)
	} else {
		m.broker.close()
	}
}
