package server

import (
	"strings"
	"testing"
)

func TestPublicAccessRequiresAdminAndSameOrigin(t *testing.T) {
	c, _ := newClient(t)
	c.expect("GET", "/api/public-access", nil, 401)
	c.expect("PUT", "/api/public-access", map[string]any{"subdomain": "office"}, 401)
	c.expect("POST", "/api/setup", map[string]string{"username": "admin", "password": "a-long-enough-password", "endpointHost": "vpn.test"}, 201)
	b := c.expect("GET", "/api/public-access", nil, 200)
	if !strings.Contains(string(b), `"available":false`) || !strings.Contains(string(b), `"loopbackOnly":true`) {
		t.Fatalf("unconfigured/loopback status incorrect: %s", b)
	}
	c.expect("PUT", "/api/public-access", map[string]any{"subdomain": "office", "autoIPv4": true}, 400)
	c.expect("PUT", "/api/public-access", map[string]any{"domain": "arbitrary.example", "token": "secret"}, 400)
	res, _ := c.do("PUT", "/api/public-access", map[string]any{"subdomain": "office"}, "Origin", "https://untrusted.test")
	if res.StatusCode != 403 {
		t.Fatalf("cross-site DNS mutation allowed: %d", res.StatusCode)
	}
	c.expect("POST", "/api/users", map[string]string{"username": "viewer", "password": "a-long-enough-password", "role": "viewer"}, 201)
	c.expect("POST", "/api/auth/logout", nil, 200)
	c.expect("POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "a-long-enough-password"}, 200)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		c.expect(method, "/api/public-access", map[string]any{}, 403)
	}
	c.expect("POST", "/api/public-access/check", map[string]any{}, 403)
}
