package update

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func releaseFixture(tag, osName, arch string) Release {
	return Release{Tag: tag, URL: "https://github.com/" + Repository + "/releases/tag/" + tag, Assets: []Asset{{Name: assetName(tag, osName, arch), URL: "https://github.com/" + Repository + "/releases/download/" + tag + "/" + assetName(tag, osName, arch), Size: 3, Digest: "sha256:" + strings.Repeat("a", 64)}}}
}

func TestSelectCompatibleReleaseAndRejectUntrustedAssets(t *testing.T) {
	good := releaseFixture("v1.1.0", "windows", "amd64")
	bad := releaseFixture("v9.0.0", "windows", "amd64")
	bad.Assets[0].URL = "https://github.com/other/project/releases/download/v9.0.0/file.exe"
	noDigest := releaseFixture("v8.0.0", "windows", "amd64")
	noDigest.Assets[0].Digest = ""
	draft := releaseFixture("v7.0.0", "windows", "amd64")
	draft.Draft = true
	pre := releaseFixture("v2.0.0-rc.1", "windows", "amd64")
	pre.Prerelease = true
	other := releaseFixture("v6.0.0", "windows", "386")
	all := []Release{bad, noDigest, draft, pre, other, good}
	c, err := chooseRelease(all, "1.0.0", "windows", "amd64", false)
	if err != nil || c == nil || c.Version != "1.1.0" {
		t.Fatalf("selection: %+v %v", c, err)
	}
	c, _ = chooseRelease(all, "1.0.0", "windows", "amd64", true)
	if c == nil || c.Version != "2.0.0-rc.1" {
		t.Fatalf("prerelease selection: %+v", c)
	}
	c, _ = chooseRelease([]Release{good}, "1.2.0", "windows", "amd64", false)
	if c != nil {
		t.Fatal("offered downgrade")
	}
	c, _ = chooseRelease(all, "1.0.0", "windows", "arm64", true)
	if c != nil {
		t.Fatal("offered unsupported Windows architecture")
	}
	for _, raw := range []string{"http://github.com/file", "https://github.com:9443/file", "https://github.com.evil.test/file", "https://user:secret@github.com/file", "https://127.0.0.1/file"} {
		u, _ := url.Parse(raw)
		if allowedDownload(u) {
			t.Fatalf("trusted %s", raw)
		}
	}
}

func TestDownloadChecksSizeAndDigest(t *testing.T) {
	for _, body := range []string{"abc", "abd", "ab", "abcd"} {
		t.Run(body, func(t *testing.T) {
			digest := sha256.Sum256([]byte("abc"))
			m := &Manager{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			c := &Candidate{Asset: Asset{URL: "https://github.com/" + Repository + "/releases/download/v1.1.0/a", Size: 3, Digest: "sha256:" + hex.EncodeToString(digest[:])}}
			err := m.download(context.Background(), c, filepath.Join(t.TempDir(), "payload"))
			if (err == nil) != (body == "abc") {
				t.Fatalf("verification(%q): %v", body, err)
			}
		})
	}
}

func TestHealthRequiresVersionAndTrustedTLS(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "1.1.0"})
	}))
	defer s.Close()
	certPath := filepath.Join(t.TempDir(), "tls.crt")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), s.URL, certPath, "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), s.URL, certPath, "1.0.0"); err == nil {
		t.Fatal("accepted wrong running version")
	}
	if err := Health(context.Background(), s.URL, "", "1.1.0"); err == nil {
		t.Fatal("accepted untrusted TLS")
	}
	r := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, s.URL, 302) }))
	defer r.Close()
	if err := Health(context.Background(), r.URL, certPath, "1.1.0"); err == nil {
		t.Fatal("followed health redirect")
	}
}

func TestUpdateLockAndUnchangedVersion(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	m := New(Options{Version: "1.1.0", DataDir: t.TempDir(), Mode: "disabled"})
	m.reason = ""
	m.mode = "systemd"
	m.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := m.Start(); err == nil {
		t.Fatal("parallel update accepted")
	}
	if !m.Status().Busy {
		t.Fatal("lost active lock")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for m.Status().Busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s := m.Status()
	if s.Busy || s.Job.Phase != "complete" {
		t.Fatalf("unchanged-version result: %+v", s)
	}
	initial := New(Options{DataDir: t.TempDir(), Mode: "disabled"}).Status()
	b, _ := json.Marshal(initial)
	if strings.Contains(string(b), "checkedAt") {
		t.Fatal("unchecked status advertised a check time")
	}
}

func TestHealthWithPinnedWildcardCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"*.vpn20.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "vpn20.vpn20.test" {
			t.Errorf("invalid wildcard SNI: %q", r.TLS.ServerName)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "1.1.0"})
	}))
	s.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	s.StartTLS()
	defer s.Close()
	certPath := filepath.Join(t.TempDir(), "fullchain.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Health(context.Background(), s.URL, certPath, "1.1.0"); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseSnapshotAndRestoreIncludesWAL(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "vpn20.db")
	snapshot := filepath.Join(dir, "backup.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting(context.Background(), "marker", "before"); err != nil {
		t.Fatal(err)
	}
	if err = backupDatabase(context.Background(), db, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting(context.Background(), "marker", "after"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err = restoreDatabase(db, snapshot); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.GetSetting(context.Background(), "marker")
	if err != nil || v != "before" {
		t.Fatalf("restored snapshot: %q %v", v, err)
	}
}

func TestApplyRejectsTamperedPayloadAndReleasesLock(t *testing.T) {
	data := t.TempDir()
	id := strings.Repeat("c", 24)
	root := filepath.Join(data, "updates")
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(dir, "payload")
	if err := os.WriteFile(payload, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "active"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Job: Job{ID: id}, DataDir: data, Payload: payload, Method: "systemd", Digest: "sha256:" + strings.Repeat("0", 64)}
	path := filepath.Join(dir, "plan.json")
	if err := writeJSON(path, plan); err != nil {
		t.Fatal(err)
	}
	if err := Apply(path); err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("did not reject tampering: %v", err)
	}
	if exists(filepath.Join(root, "active")) || readJob(root).Phase != "failed" {
		t.Fatal("tampered payload left a permanent update lock or hid failure")
	}
}
