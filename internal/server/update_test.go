package server

import (
	"strings"
	"testing"
)

func TestUpdaterRequiresAdminAndSameOrigin(t *testing.T) {
	c, _ := newClient(t)
	c.expect("GET", "/api/update", nil, 401)
	c.expect("POST", "/api/update", map[string]string{"url": "https://untrusted.test/program"}, 401)
	c.expect("POST", "/api/setup", map[string]string{"username": "admin", "password": "a-long-enough-password", "endpointHost": "vpn.test"}, 201)
	b := c.expect("GET", "/api/update", nil, 200)
	if !strings.Contains(string(b), `"supported":false`) {
		t.Fatalf("mock test should not run host updates: %s", b)
	}
	c.expect("POST", "/api/update", map[string]string{}, 409)
	res, _ := c.do("POST", "/api/update", map[string]string{}, "Origin", "https://untrusted.test")
	if res.StatusCode != 403 {
		t.Fatalf("cross-site update: %d", res.StatusCode)
	}
	c.expect("POST", "/api/users", map[string]string{"username": "viewer", "password": "a-long-enough-password", "role": "viewer"}, 201)
	c.expect("POST", "/api/auth/logout", nil, 200)
	c.expect("POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "a-long-enough-password"}, 200)
	c.expect("GET", "/api/update", nil, 403)
	c.expect("POST", "/api/update/check", map[string]string{}, 403)
	c.expect("POST", "/api/update", map[string]string{}, 403)
}
