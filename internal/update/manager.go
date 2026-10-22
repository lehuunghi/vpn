package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Version, DataDir, DBPath, Mode, DockerSocket, DockerContainer string
	HTTPURL, TLSCert, CertificateThumbprint                       string
	Prerelease                                                    bool
}

type Job struct {
	ID        string    `json:"id,omitempty"`
	Phase     string    `json:"phase"`
	Version   string    `json:"version,omitempty"`
	Message   string    `json:"message,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Status struct {
	Current   string       `json:"current"`
	Method    string       `json:"method"`
	Supported bool         `json:"supported"`
	Reason    string       `json:"reason,omitempty"`
	Busy      bool         `json:"busy"`
	Latest    *Candidate   `json:"latest,omitempty"`
	CheckedAt time.Time    `json:"checkedAt,omitzero"`
	Job       Job          `json:"job"`
	Signing   *SigningInfo `json:"signing,omitempty"`
}

type SigningInfo struct {
	Status      string `json:"status"`
	Publisher   string `json:"publisher,omitempty"`
	Timestamped bool   `json:"timestamped"`
}

type Manager struct {
	opts               Options
	mode, root, reason string
	client             *http.Client
	mu                 sync.Mutex
	checkMu            sync.Mutex
	candidate          *Candidate
	checkedAt          time.Time
	signing            *SigningInfo
}

func New(opts Options) *Manager {
	if path, err := filepath.Abs(opts.DataDir); err == nil {
		opts.DataDir = path
	}
	if path, err := filepath.Abs(opts.DBPath); err == nil {
		opts.DBPath = path
	}
	m := &Manager{opts: opts, root: filepath.Join(opts.DataDir, "updates"), client: releaseClient()}
	m.mode = opts.Mode
	if m.mode == "" || m.mode == "auto" {
		switch {
		case runtime.GOOS == "windows":
			m.mode = "windows"
		case exists("/.dockerenv"):
			m.mode = "docker"
		case runtime.GOOS == "linux":
			m.mode = "systemd"
		default:
			m.mode = "disabled"
		}
	}
	if m.opts.DockerSocket == "" {
		m.opts.DockerSocket = "/var/run/docker.sock"
	}
	if m.mode == "docker" {
		if !exists(m.opts.DockerSocket) {
			m.reason = "Cần bật cấu hình cập nhật Docker và gắn Docker socket theo hướng dẫn cài đặt."
		}
	} else {
		m.reason = nativeAvailable(m.mode)
	}
	if m.mode == "windows" && m.reason == "" {
		m.signing = installedSignature()
	}
	// A dead download/helper must not permanently disable updates after reboot.
	if j := readJob(m.root); exists(filepath.Join(m.root, "active")) && time.Since(j.UpdatedAt) > 30*time.Minute {
		_ = os.Remove(filepath.Join(m.root, "active"))
		_ = writeJob(m.root, Job{ID: j.ID, Phase: "failed", Version: j.Version, Message: "Lần cập nhật trước bị gián đoạn. Kiểm tra nhật ký và bản sao lưu trước khi thử lại."})
	}
	return m
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := readJob(m.root)
	if j.Phase == "" {
		j.Phase = "idle"
	}
	return Status{Current: m.opts.Version, Method: m.mode, Supported: m.reason == "", Reason: m.reason, Busy: exists(filepath.Join(m.root, "active")), Latest: m.candidate, CheckedAt: m.checkedAt, Job: j, Signing: m.signing}
}

func (m *Manager) Check(ctx context.Context) (Status, error) {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()
	m.mu.Lock()
	cached := !m.checkedAt.IsZero() && time.Since(m.checkedAt) < 30*time.Second
	m.mu.Unlock()
	if cached {
		return m.Status(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := m.latest(ctx)
	if err != nil {
		return m.Status(), err
	}
	m.mu.Lock()
	m.candidate = c
	m.checkedAt = time.Now()
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *Manager) Start() (Job, error) {
	if m.reason != "" {
		return Job{}, errors.New(m.reason)
	}
	if err := os.MkdirAll(m.root, 0700); err != nil {
		return Job{}, err
	}
	lock, err := os.OpenFile(filepath.Join(m.root, "active"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Job{}, errors.New("Một lần cập nhật đang chạy; hãy chờ hoàn tất.")
	}
	_ = lock.Close()
	b := make([]byte, 12)
	if _, err = rand.Read(b); err != nil {
		_ = os.Remove(filepath.Join(m.root, "active"))
		return Job{}, err
	}
	j := Job{ID: hex.EncodeToString(b), Phase: "checking", Message: "Đang kiểm tra phiên bản mới nhất."}
	if err = writeJob(m.root, j); err != nil {
		_ = os.Remove(filepath.Join(m.root, "active"))
		return Job{}, err
	}
	go m.run(j)
	return j, nil
}

var errHandedOff = errors.New("update handed to detached helper")

func (m *Manager) run(j Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := m.prepare(ctx, &j)
	if errors.Is(err, errHandedOff) {
		return
	}
	if err != nil {
		j.Phase = "failed"
		j.Message = err.Error()
	} else {
		j.Phase = "complete"
		j.Message = "Đang dùng phiên bản mới nhất."
	}
	_ = writeJob(m.root, j)
	_ = os.Remove(filepath.Join(m.root, "active"))
}

func (m *Manager) prepare(ctx context.Context, j *Job) error {
	if _, err := m.Check(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	c := m.candidate
	m.mu.Unlock()
	if c == nil {
		return nil
	}
	j.Version = c.Version
	j.Phase = "downloading"
	j.Message = "Đang tải và xác minh phiên bản mới."
	_ = writeJob(m.root, *j)
	if m.mode == "docker" {
		return m.prepareDocker(ctx, j, c)
	}
	dir := filepath.Join(m.root, j.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	payload := filepath.Join(dir, c.Asset.Name)
	if err := m.download(ctx, c, payload); err != nil {
		return err
	}
	if err := verifyNativePayload(ctx, payload, m.opts.CertificateThumbprint); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	p := Plan{Job: *j, DataDir: m.opts.DataDir, DBPath: m.opts.DBPath, Method: m.mode, Version: c.Version, Executable: exe, Payload: payload, Digest: c.Asset.Digest, HTTPURL: m.opts.HTTPURL, TLSCert: m.opts.TLSCert, CertificateThumbprint: m.opts.CertificateThumbprint}
	planPath := filepath.Join(dir, "plan.json")
	if err = writeJSON(planPath, p); err != nil {
		return err
	}
	helper := filepath.Join(dir, "vpn20-helper"+map[bool]string{true: ".exe"}[runtime.GOOS == "windows"])
	if err = copyFile(exe, helper, 0700); err != nil {
		return err
	}
	j.Phase = "installing"
	j.Message = "Đang sao lưu và cài đặt. Giao diện sẽ tự kết nối lại sau khi dịch vụ khởi động."
	_ = writeJob(m.root, *j)
	if err = launchHelper(helper, planPath, filepath.Join(dir, "update.log")); err != nil {
		return err
	}
	return errHandedOff
}

func readJob(root string) Job {
	var j Job
	b, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err == nil {
		_ = json.Unmarshal(b, &j)
	}
	return j
}

func writeJob(root string, j Job) error {
	j.UpdatedAt = time.Now().UTC()
	return writeJSON(filepath.Join(root, "state.json"), j)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".update-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyFile(src, dst string, mode os.FileMode) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular update file: %s", src)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = out.ReadFrom(f)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
