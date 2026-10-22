package update

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lehuunghi/vpn/internal/store"
)

type Plan struct {
	Job                                                           Job
	DataDir, DBPath, Method, Version, Executable, Payload, Digest string
	HTTPURL, TLSCert, CertificateThumbprint                       string
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Apply runs in a detached copy of the old executable, outside the installation
// directory. It survives a service stop and can restore the previous binary/DB.
func Apply(planPath string) (result error) {
	b, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var p Plan
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	root := filepath.Join(p.DataDir, "updates")
	dir := filepath.Join(root, p.Job.ID)
	if len(p.Job.ID) != 24 || strings.ContainsAny(p.Job.ID, "/\\.") || !filepath.IsAbs(p.DataDir) || filepath.Clean(planPath) != filepath.Join(dir, "plan.json") || !pathInside(dir, p.Payload) {
		return errors.New("invalid update plan location")
	}
	if p.Method != "windows" && p.Method != "systemd" {
		return errors.New("invalid native update method")
	}
	defer func() {
		if result != nil {
			p.Job.Phase = "failed"
			p.Job.Message = result.Error()
			_ = writeJob(root, p.Job)
		}
		_ = os.Remove(filepath.Join(root, "active"))
	}()
	if got, err := fileDigest(p.Payload); err != nil || got != p.Digest {
		return errors.New("staged update digest changed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err = verifyNativePayload(ctx, p.Payload, p.CertificateThumbprint); err != nil {
		return err
	}
	// Give the request handler time to finish the accepted response and audit.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	p.Job.Phase = "backing_up"
	p.Job.Message = "Đang dừng dịch vụ và sao lưu chương trình, dữ liệu."
	_ = writeJob(root, p.Job)
	if err = stopNative(ctx, p); err != nil {
		return err
	}
	backup := filepath.Join(dir, "backup")
	if err = os.MkdirAll(backup, 0700); err == nil {
		err = backupNative(p, backup)
	}
	if err == nil {
		err = backupDatabase(ctx, p.DBPath, filepath.Join(backup, "vpn20.db"))
	}
	if err != nil {
		restartCtx, restartCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer restartCancel()
		if restartErr := startNative(restartCtx, p); restartErr != nil {
			return fmt.Errorf("backup failed (%v); restart failed: %w", err, restartErr)
		}
		return fmt.Errorf("backup failed; update cancelled: %w", err)
	}
	p.Job.Phase = "installing"
	p.Job.Message = "Đang thay toàn bộ phiên bản chương trình."
	_ = writeJob(root, p.Job)
	err = installNative(ctx, p)
	if err == nil {
		err = startNative(ctx, p)
	}
	if err == nil {
		p.Job.Phase = "restarting"
		p.Job.Message = "Đang kiểm tra phiên bản mới sau khi khởi động."
		_ = writeJob(root, p.Job)
		err = waitHealthy(ctx, p.HTTPURL, p.TLSCert, p.Version, 2*time.Minute)
	}
	if err == nil {
		p.Job.Phase = "complete"
		p.Job.Message = "Đã cập nhật đầy đủ và kiểm tra khởi động thành công."
		return writeJob(root, p.Job)
	}
	updateErr := err
	p.Job.Phase = "rolling_back"
	p.Job.Message = "Phiên bản mới không khởi động được; đang khôi phục bản trước."
	_ = writeJob(root, p.Job)
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer rollbackCancel()
	err = stopNative(rollbackCtx, p)
	if err == nil {
		err = restoreNative(p, backup)
	}
	if err == nil {
		err = restoreDatabase(p.DBPath, filepath.Join(backup, "vpn20.db"))
	}
	if err == nil {
		err = startNative(rollbackCtx, p)
	}
	if err == nil {
		err = waitHealthy(rollbackCtx, p.HTTPURL, p.TLSCert, "", time.Minute)
	}
	if err != nil {
		return fmt.Errorf("update failed (%v); automatic rollback failed (%v); backup: %s", updateErr, err, backup)
	}
	p.Job.Phase = "rolled_back"
	p.Job.Message = "Đã khôi phục phiên bản và dữ liệu trước cập nhật: " + updateErr.Error()
	return writeJob(root, p.Job)
}

func backupDatabase(ctx context.Context, path, dest string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("database is not a regular file")
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err = st.DB().ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0600)
}

func restoreDatabase(path, backup string) error {
	if err := copyFile(backup, path, 0600); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func LocalURL(listen string, tlsEnabled bool) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/api/health"
}

// Health verifies TLS against the configured local certificate, never by
// disabling certificate validation. It also checks the running binary version.
func Health(ctx context.Context, address, certificate, version string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("invalid health address")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if u.Scheme == "https" && certificate != "" {
		b, err := os.ReadFile(certificate)
		if err != nil {
			return err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			return errors.New("invalid health TLS certificate")
		}
		tlsConfig := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		if block, _ := pem.Decode(b); block != nil {
			if leaf, err := x509.ParseCertificate(block.Bytes); err == nil && len(leaf.DNSNames) > 0 {
				tlsConfig.ServerName = leaf.DNSNames[0]
			}
		}
		transport.TLSClientConfig = tlsConfig
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 4 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("health redirect refused") }}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body) != nil || !body.OK || (version != "" && body.Version != version) {
		return errors.New("health response or version check failed")
	}
	return nil
}

func waitHealthy(ctx context.Context, address, cert, version string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		if last = Health(ctx, address, cert, version); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("startup health check: %w (%v)", ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
}
