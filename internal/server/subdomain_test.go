package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/config"
)

func TestSubdomainRequiresAdminAndSameOrigin(t *testing.T) {
	c, _ := newClient(t)
	c.expect("GET", "/api/subdomain", nil, 401)
	c.expect("POST", "/api/subdomain", map[string]string{"name": "office"}, 401)
	c.expect("POST", "/api/setup", map[string]string{"username": "admin", "password": "long-enough-password", "endpointHost": "vpn.test"}, 201)
	out := c.expect("GET", "/api/subdomain", nil, 200)
	if strings.Contains(string(out), "keys") || strings.Contains(string(out), "token") {
		t.Fatal("credential leaked")
	}
	c.expect("POST", "/api/subdomain", map[string]string{"name": "a.b"}, 400)
	res, _ := c.do("POST", "/api/subdomain", map[string]string{"name": "office"}, "Sec-Fetch-Site", "cross-site")
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	c.expect("POST", "/api/users", map[string]string{"username": "viewer", "password": "long-enough-password", "role": "viewer"}, 201)
	c.expect("POST", "/api/auth/logout", nil, 200)
	c.expect("POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "long-enough-password"}, 200)
	c.expect("GET", "/api/subdomain", nil, 403)
	c.expect("POST", "/api/subdomain", map[string]string{"name": "office"}, 403)
}
func TestSessionCookieFollowsActualHTTPSWithoutBreakingLAN(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	for _, secure := range []bool{false, true} {
		request := httptest.NewRequest("GET", "http://192.168.1.100:51821", nil)
		if secure {
			request.TLS = &tls.ConnectionState{}
		}
		recorder := httptest.NewRecorder()
		s.setCookie(recorder, request, "secret", time.Now().Add(time.Hour))
		cookies := recorder.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Secure != secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("%+v", cookies)
		}
		clear := httptest.NewRecorder()
		s.clearCookie(clear, request)
		if c := clear.Result().Cookies()[0]; c.Secure != secure || c.MaxAge != -1 {
			t.Fatal(c)
		}
	}
}
