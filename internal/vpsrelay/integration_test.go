package vpsrelay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	_ "github.com/fatedier/frp/server" // Keep CLI fixture dependencies pinned by go mod tidy.
)

func startFRPS(t *testing.T, cfg *v1.ServerConfig, dir string) {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "frps"+suffix)
	// Run the same native CLI as a deployed VPS in a separate process. Race
	// instrumentation in this test remains on the VPN20 client and policy.
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-race=false", "-o", binary, "./testdata/frps")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build frps fixture: %v\n%s", err, output)
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "frps.json")
	if err := os.WriteFile(config, body, 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-c", config)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
}

func waitStatus(t *testing.T, m *Manager, match func(Status) bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if match(m.Snapshot()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("relay did not reach expected state: %+v", m.Snapshot())
}

func TestRealTLSUDPReconnectionAndProfileIsolation(t *testing.T) {
	// Real frps sockets and a profile policy with the same Login/NewProxy/
	// Ping/NewWorkConn contract as deploy/relay/relay.py. No external service.
	ca, key := fixtureCertificate(t)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, ca, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	control := tcp.Addr().(*net.TCPAddr).Port
	_ = tcp.Close()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote := udp.LocalAddr().(*net.UDPAddr).Port
	_ = udp.Close()
	echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buf[:n], addr)
		}
	}()
	var allowed atomic.Bool
	allowed.Store(true)
	var mu sync.Mutex
	session := ""
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Content map[string]any `json:"content"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		content := input.Content
		ok := allowed.Load()
		result := map[string]any{"unchange": true}
		op := r.URL.Query().Get("op")
		mu.Lock()
		switch op {
		case "Login":
			metas, _ := content["metas"].(map[string]any)
			ok = ok && content["user"] == "office" && metas["access_key"] == strings.Repeat("a", 64)
			if ok {
				var id [16]byte
				_, _ = rand.Read(id[:])
				session = hex.EncodeToString(id[:])
				content["run_id"] = session
				delete(metas, "access_key")
				result = map[string]any{"unchange": false, "content": content}
			}
		default:
			user, _ := content["user"].(map[string]any)
			ok = ok && user["user"] == "office" && user["run_id"] == session && session != ""
			if op == "NewProxy" {
				ok = ok && content["proxy_type"] == "udp" && content["proxy_name"] == "office.vpn20" && content["remote_port"] == float64(remote)
			} else if op == "NewWorkConn" {
				ok = ok && content["run_id"] == session
			}
		}
		mu.Unlock()
		if !ok {
			result = map[string]any{"reject": true, "reject_reason": "relay authorization denied"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer policy.Close()
	cfg := &v1.ServerConfig{BindAddr: "127.0.0.1", BindPort: control,
		Auth:        v1.AuthServerConfig{Method: "token", Token: strings.Repeat("b", 64), AdditionalScopes: []v1.AuthScope{"HeartBeats", "NewWorkConns"}},
		Transport:   v1.ServerTransportConfig{TLS: v1.TLSServerConfig{Force: true, TLSConfig: v1.TLSConfig{CertFile: certFile, KeyFile: keyFile}}},
		HTTPPlugins: []v1.HTTPPluginOptions{{Name: "profiles", Addr: strings.TrimPrefix(policy.URL, "http://"), Path: "/authorize", Ops: []string{"Login", "NewProxy", "Ping", "NewWorkConn"}}}}
	cfg.Complete()
	startFRPS(t, cfg, dir)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts := Options{Store: &memoryStore{}, LocalPort: echo.LocalAddr().(*net.UDPAddr).Port,
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(control)))

		}}
	m := New(opts)
	defer func() { m.Close() }()
	m.Start(ctx)
	archive := zipProfile(t, profileConfig(control, remote), ca, nil)
	if _, err := m.Configure(ctx, Input{Enabled: true, ProfileZIP: archive}); err != nil {
		t.Fatal(err)
	}
	connected := func(s Status) bool { return s.Phase == "connected" }
	waitStatus(t, m, connected)
	exchange := func() {
		t.Helper()
		target := net.JoinHostPort("127.0.0.1", strconv.Itoa(remote))
		conn, err := net.Dial("udp4", target)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		for _, size := range []int{32, 148, 1452} {
			payload := make([]byte, size)
			_, _ = rand.Read(payload)
			if _, err := conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 65535)
			n, err := conn.Read(buf)
			if err != nil || string(buf[:n]) != string(payload) {
				t.Fatalf("UDP %d: %v", size, err)
			}
		}
	}
	exchange() // Profile's 192.0.2.100:9 is ignored; actual local VPN port wins.
	m.Close()
	m = New(opts)
	m.Start(ctx)
	waitStatus(t, m, connected)
	exchange() // Saved profile resumes after a VPN20 restart.
	if _, err := m.Configure(ctx, Input{Enabled: true, Domain: "wrong.example.test"}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, func(s Status) bool { return s.Phase == "error" && strings.Contains(s.Message, "SSL") })
	if _, err := m.Configure(ctx, Input{Enabled: true, Domain: "relay.example.test"}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, connected)
	exchange()
	allowed.Store(false)
	waitStatus(t, m, func(s Status) bool { return s.Phase != "connected" })
	if _, _, on := m.Endpoint(); !on {
		t.Fatal("outage silently selected a direct endpoint")
	}
	allowed.Store(true)
	waitStatus(t, m, connected)
	exchange()
	if _, err := m.Configure(ctx, Input{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, _, on := m.Endpoint(); on {
		t.Fatal("disable kept relay enabled")
	}
	bad := zipProfile(t, strings.ReplaceAll(profileConfig(control, remote), strings.Repeat("a", 64), strings.Repeat("c", 64)), ca, nil)
	if _, err := m.Configure(ctx, Input{Enabled: true, ProfileZIP: bad}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, func(s Status) bool { return s.Phase == "error" })
	if m.Snapshot().Phase == "connected" {
		t.Fatal("wrong profile gained access")
	}
}
