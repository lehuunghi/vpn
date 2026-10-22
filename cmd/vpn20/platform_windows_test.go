//go:build windows

package main

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehuunghi/vpn/internal/config"
	"github.com/lehuunghi/vpn/internal/engine"
)

func TestWindowsStopIgnoresBrokenConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("broken json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN20_CONFIG", path)
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	os.Args = []string{"vpn20.exe", "stop-service"}
	if err := platformInit(); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"install-service", "enable-lan"} {
		os.Args = []string{"vpn20.exe", command}
		if err := platformInit(); err == nil {
			t.Fatal("invalid configuration was accepted for startup")
		}
		body, err := os.ReadFile(filepath.Join(filepath.Dir(path), "install-error.txt"))
		if err != nil || !strings.Contains(string(body), "config.json") {
			t.Fatalf("%s lost configuration error: %s %v", command, body, err)
		}
	}
}

func TestWindowsInstallErrorIncludesBoundedLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VPN20_CONFIG", filepath.Join(dir, "config.json"))
	if err := os.WriteFile(filepath.Join(dir, "vpn20.log"), []byte(strings.Repeat("old entry\n", 2000)+"driver startup failed"), 0600); err != nil {
		t.Fatal(err)
	}
	writeInstallResult(errors.New("service not ready"))
	body, err := os.ReadFile(filepath.Join(dir, "install-error.txt"))
	if err != nil || len(body) > 4000 || !strings.Contains(string(body), "service not ready") || !strings.Contains(string(body), "driver startup failed") {
		t.Fatalf("bad installer diagnostics: %d bytes, %v", len(body), err)
	}
	writeInstallResult(nil)
	if _, err := os.Stat(filepath.Join(dir, "install-error.txt")); !os.IsNotExist(err) {
		t.Fatal("successful startup left stale diagnostics")
	}
}

func TestWindowsConfigPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"VPN20_ENDPOINT":"file.example","VPN20_PORT":"51900"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN20_CONFIG", path)
	t.Setenv("VPN20_ENDPOINT", "env.example")
	t.Setenv("VPN20_PORT", "")
	_ = os.Unsetenv("VPN20_PORT")
	if err := platformInit(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VPN20_ENDPOINT") != "env.example" || os.Getenv("VPN20_PORT") != "51900" {
		t.Fatal("configuration precedence is incorrect")
	}
}

func TestWindowsHealthCustomListener(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": engine.Version})
	})
	s := httptest.NewServer(handler)
	defer s.Close()
	cfg := &config.Config{HTTP: strings.TrimPrefix(s.URL, "http://")}
	if !windowsHealthReady(cfg) {
		t.Fatal("custom HTTP port did not become ready")
	}
	tlsServer := httptest.NewTLSServer(handler)
	defer tlsServer.Close()
	certFile := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	cfg = &config.Config{HTTP: strings.TrimPrefix(tlsServer.URL, "https://"), TLSCert: certFile, TLSKey: "unused-by-probe"}
	if !windowsHealthReady(cfg) {
		t.Fatal("installed TLS certificate was not trusted")
	}
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("other application")) }))
	defer wrong.Close()
	if windowsHealthReady(&config.Config{HTTP: strings.TrimPrefix(wrong.URL, "http://")}) {
		t.Fatal("an unrelated HTTP listener was accepted as VPN20")
	}
}
