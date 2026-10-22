package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudflareTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("private-test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN20_CLOUDFLARE_API_TOKEN_FILE", path)
	t.Setenv("VPN20_CLOUDFLARE_API_TOKEN", "")
	t.Setenv("VPN20_CLOUDFLARE_DOMAIN", "Example.COM.")
	c, err := FromEnv()
	if err != nil || c.CloudflareToken != "private-test-token" || c.CloudflareDomain != "example.com" {
		t.Fatal("token file or domain normalization failed", err)
	}
	t.Setenv("VPN20_CLOUDFLARE_API_TOKEN", "another-test-token")
	if _, err = FromEnv(); err == nil || strings.Contains(err.Error(), "private-test-token") || strings.Contains(err.Error(), "another-test-token") {
		t.Fatal("ambiguous token sources accepted or exposed", err)
	}
}
