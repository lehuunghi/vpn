package ddns

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

const stateKey = "ddns.v2"

type Store interface {
	GetSetting(context.Context, string) (string, error)
	SetSetting(context.Context, string, string) error
}
type Options struct {
	APIURL, AdminAddress, DataDir, ACMECA string
	Store                                 Store
	Activate                              func(context.Context, string) error
	OpenFirewall                          func(context.Context, string) error
	Log                                   *slog.Logger
	interval                              time.Duration
	factory                               func(*Manager) (certificates, error)
}
type Status struct {
	Name            string     `json:"name"`
	Hostname        string     `json:"hostname,omitempty"`
	URL             string     `json:"url,omitempty"`
	Busy            bool       `json:"busy"`
	Phase           string     `json:"phase"`
	Message         string     `json:"message,omitempty"`
	SSLReady        bool       `json:"sslReady"`
	TestCertificate bool       `json:"testCertificate,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	LastUpdate      *time.Time `json:"lastUpdate,omitempty"`
	IPv4            string     `json:"ipv4,omitempty"`
	IPv6            string     `json:"ipv6,omitempty"`
	// DNS/SSL success does not claim NAT, firewall or Internet reachability.
	Reachability string `json:"reachability"`
}
type savedState struct {
	Active  string            `json:"active"`
	Pending string            `json:"pending"`
	Keys    map[string]string `json:"keys"`
	Retire  []string          `json:"retire,omitempty"`
}
type Manager struct {
	opts                Options
	broker              *broker
	mu                  sync.Mutex
	state               savedState
	status              Status
	wake                chan struct{}
	done                chan struct{}
	cancel              context.CancelFunc
	certs               certificates
	address             string
	retryAt             time.Time
	certificateFailures int
	certificateFailure  string
	publicHTTPS         bool
}

var labelPattern = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")

func Label(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if !labelPattern.MatchString(name) {
		return "", errors.New("Tên chỉ gồm a-z, 0-9 và dấu gạch nối; tối đa 63 ký tự")
	}
	switch name {
	case "api", "ddns", "admin", "www", "mail", "ns", "ns1", "ns2", "acme", "auth":
		return "", errors.New("Tên này được dành riêng")
	}
	return name, nil
}
func New(opts Options) *Manager {
	if opts.APIURL == "" {
		opts.APIURL = DefaultAPI
	}
	if opts.interval == 0 {
		opts.interval = 2 * time.Minute
	}
	if opts.factory == nil {
		opts.factory = newCertificates
	}
	m := &Manager{opts: opts, address: opts.AdminAddress, wake: make(chan struct{}, 1), done: make(chan struct{})}
	m.status = Status{Phase: "idle", Reachability: "not_checked"}
	m.state.Keys = map[string]string{}
	var err error
	m.broker, err = newBroker(opts.APIURL)
	if err == nil && opts.Store != nil {
		var raw string
		raw, err = opts.Store.GetSetting(context.Background(), stateKey)
		if err == nil && raw != "" {
			err = json.Unmarshal([]byte(raw), &m.state)
		}
	}
	if m.state.Keys == nil {
		m.state.Keys = map[string]string{}
	}
	if err != nil {
		m.status.Phase = "error"
		m.status.Message = "Không đọc được cấu hình DDNS; kiểm tra cấu hình dịch vụ"
	}
	return m
}
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	go m.run(ctx)
}
func (m *Manager) Close() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-m.done
}
func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	status := m.status
	state := m.state
	certs := m.certs
	m.mu.Unlock()
	name := state.Pending
	if name == "" {
		name = state.Active
	}
	status.Name = strings.TrimSuffix(name, "."+Domain)
	status.Hostname = state.Active
	if state.Active != "" {
		m.mu.Lock()
		address := m.address
		m.mu.Unlock()
		if address != "" {
			_, port, _ := net.SplitHostPort(address)
			status.URL = "https://" + state.Active
			if port != "443" {
				status.URL += ":" + port
			}
		}
	}
	if certs != nil && state.Active != "" {
		expiry := certs.Expires(state.Active)
		status.SSLReady = !expiry.IsZero() && time.Now().Before(expiry)
		if !expiry.IsZero() {
			status.ExpiresAt = &expiry
		}
	}
	return status
}
func (m *Manager) Configure(ctx context.Context, value string) (Status, error) {
	label, err := Label(value)
	if err != nil {
		return m.Snapshot(), err
	}
	hostname := label + "." + Domain
	m.mu.Lock()
	if m.cancel == nil || m.broker == nil || m.opts.Store == nil {
		m.mu.Unlock()
		return m.Snapshot(), errors.New("Dịch vụ DDNS chưa sẵn sàng")
	}
	if m.status.Busy {
		m.mu.Unlock()
		return m.Snapshot(), errors.New("Đang cập nhật subdomain; chờ hoàn thành")
	}
	// Work on a copy so a persistence failure never loses the previous key.
	next := m.state
	next.Keys = make(map[string]string, len(m.state.Keys)+1)
	for k, v := range m.state.Keys {
		next.Keys[k] = v
	}
	if next.Keys[hostname] == "" {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			m.mu.Unlock()
			return m.Snapshot(), errors.New("Không tạo được khóa máy")
		}
		next.Keys[hostname] = hex.EncodeToString(secret[:])
	}
	next.Pending = hostname
	if err := m.save(ctx, next); err != nil {
		m.mu.Unlock()
		return m.Snapshot(), errors.New("Không lưu được cấu hình DDNS")
	}
	m.state = next
	m.retryAt = time.Time{} // Explicit user retry may follow a corrected Worker.
	m.certificateFailure = ""
	m.status.Busy = true
	m.status.Phase = "updating"
	m.status.Message = "Đang đăng ký tên và cập nhật IP…"
	// Publish the wake-up with the state. Startup must not observe the new
	// pending name before its notification and run the same claim twice.
	select {
	case m.wake <- struct{}{}:
	default:
	}
	m.mu.Unlock()
	return m.Snapshot(), nil
}
func (m *Manager) save(ctx context.Context, state savedState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return m.opts.Store.SetSetting(ctx, stateKey, string(data))
}
func (m *Manager) key(hostname string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := Label(strings.TrimSuffix(hostname, "."+Domain)); err != nil || !strings.HasSuffix(hostname, "."+Domain) {
		return ""
	}
	return m.state.Keys[hostname]
}
func (m *Manager) run(ctx context.Context) {
	defer close(m.done)
	defer func() {
		if m.certs != nil {
			m.certs.Close()
		}
		if m.broker != nil {
			m.broker.close()
		}
	}()
	ticker := time.NewTicker(m.opts.interval)
	defer ticker.Stop()
	m.mu.Lock()
	hasState := m.state.Active != "" || m.state.Pending != ""
	m.mu.Unlock()
	if hasState {
		select {
		case <-m.wake:
		default:
		}
		m.reconcile(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
		m.reconcile(ctx)
	}
}
func (m *Manager) fail(message string) {
	m.mu.Lock()
	m.status.Busy = false
	m.status.Phase = "error"
	m.status.Message = message
	m.mu.Unlock()
}
func (m *Manager) sync(ctx context.Context, hostname string) error {
	ip4, ip6, err := m.broker.sync(ctx, m.key(hostname))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.status.LastUpdate = &now
	m.status.IPv4 = ip4
	m.status.IPv6 = ip6
	m.mu.Unlock()
	return nil
}

// SetAdminAddress records the existing API listener; DDNS never opens a socket.
func (m *Manager) SetAdminAddress(address string) {
	m.mu.Lock()
	m.address = address
	m.mu.Unlock()
}

func (m *Manager) IsActiveHostname(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Active != "" && strings.EqualFold(host, m.state.Active)
}

func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	hostname, certs := m.state.Active, m.certs
	m.mu.Unlock()
	if certs == nil || hostname == "" || !strings.EqualFold(hello.ServerName, hostname) {
		return nil, errors.New("Chứng chỉ cho hostname này chưa sẵn sàng")
	}
	return certs.GetCertificate(hello)
}

func (m *Manager) ensureCertificates() error {
	if m.certs != nil {
		return nil
	}
	certs, err := m.opts.factory(m)
	if err != nil {
		return errors.New("Không khởi tạo được bộ cấp SSL")
	}
	m.mu.Lock()
	m.certs = certs
	m.mu.Unlock()
	return nil
}

func (m *Manager) enablePublicHTTPS(ctx context.Context) error {
	if m.publicHTTPS || m.opts.OpenFirewall == nil {
		return nil
	}
	m.mu.Lock()
	address := m.address
	m.mu.Unlock()
	if err := m.opts.OpenFirewall(ctx, address); err != nil {
		return errors.New("Không mở được firewall cho HTTPS trên cổng quản trị")
	}
	m.publicHTTPS = true
	return nil
}

func (m *Manager) reconcile(ctx context.Context) {
	m.mu.Lock()
	active, pending := m.state.Active, m.state.Pending
	if active == "" && pending == "" {
		m.mu.Unlock()
		return
	}
	m.status.Busy = true
	m.status.Phase = "updating"
	m.status.Message = "Đang cập nhật IP…"
	m.mu.Unlock()
	if m.broker == nil {
		m.fail("Địa chỉ API DDNS không hợp lệ")
		return
	}
	if err := m.ensureCertificates(); err != nil {
		m.fail(err.Error())
		return
	}
	// Keep the currently working name alive even if a requested rename fails.
	if active != "" && pending != active {
		certCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		certErr := m.certs.Manage(certCtx, active)
		cancel()
		// Cached HTTPS remains reachable even during a broker outage. An SSL
		// renewal failure must not stop the dynamic-IP updates for this name.
		if certErr == nil {
			if err := m.enablePublicHTTPS(ctx); err != nil && pending == "" {
				m.fail(err.Error())
				return
			}
		}
		if err := m.sync(ctx, active); err != nil && pending == "" {
			m.fail(err.Error())
			return
		}
		if certErr != nil && pending == "" {
			if m.opts.Log != nil {
				m.opts.Log.Warn("automatic HTTPS certificate failed", "hostname", active, "error", certErr)
			}
			m.fail(certificateMessage(certErr))
			return
		}
	}
	if pending != "" {
		if err := m.broker.claim(ctx, strings.TrimSuffix(pending, "."+Domain), m.key(pending)); err != nil {
			m.fail(err.Error())
			return
		}
		if err := m.sync(ctx, pending); err != nil {
			m.fail(err.Error())
			return
		}
		m.mu.Lock()
		retryAt := m.retryAt
		failure := m.certificateFailure
		m.mu.Unlock()
		if time.Now().Before(retryAt) {
			m.fail(failure + " · Đang chờ thử lại cấp SSL tự động")
			return
		}
		m.mu.Lock()
		m.status.Phase = "ssl"
		m.status.Message = "Đang cấp SSL…"
		m.mu.Unlock()
		certCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		err := m.certs.Manage(certCtx, pending)
		cancel()
		if err != nil {
			m.certificateFailed(pending, err)
			return
		}
		m.mu.Lock()
		m.certificateFailures = 0
		m.certificateFailure = ""
		m.retryAt = time.Time{}
		next := m.state
		next.Active = pending
		next.Pending = ""
		if active != "" && active != pending {
			next.Retire = append(append([]string{}, next.Retire...), active)
		}
		err = m.save(ctx, next)
		if err == nil {
			m.state = next
		}
		m.mu.Unlock()
		if err != nil {
			m.fail("Đã cấp SSL nhưng chưa lưu được cấu hình; sẽ tự thử lại")
			return
		}
		if err := m.enablePublicHTTPS(ctx); err != nil {
			m.fail(err.Error())
			return
		}
		if m.opts.Activate != nil {
			if err := m.opts.Activate(ctx, pending); err != nil {
				m.fail("Đã cấp SSL nhưng chưa cập nhật được Endpoint VPN; sẽ tự thử lại")
				return
			}
		}
		active = pending
	} else if active != "" && m.opts.Activate != nil {
		if err := m.enablePublicHTTPS(ctx); err != nil {
			m.fail(err.Error())
			return
		}
		if err := m.opts.Activate(ctx, active); err != nil {
			m.fail("Chưa đồng bộ được Endpoint VPN")
			return
		}
	}
	m.retire(ctx)
	m.mu.Lock()
	_, port, _ := net.SplitHostPort(m.address)
	m.status.URL = "https://" + active
	if port != "443" {
		m.status.URL += ":" + port
	}
	m.status.Busy = false
	m.status.Phase = "ready"
	m.status.Message = "SSL sẵn sàng · IP tự cập nhật"
	m.status.TestCertificate = m.opts.ACMECA != "" && m.opts.ACMECA != "https://acme-v02.api.letsencrypt.org/directory"
	if m.status.TestCertificate {
		m.status.Message = "Chứng chỉ thử nghiệm · IP tự cập nhật"
	}
	m.mu.Unlock()
}
func (m *Manager) retire(ctx context.Context) {
	m.mu.Lock()
	names := append([]string{}, m.state.Retire...)
	active := m.state.Active
	m.mu.Unlock()
	for _, name := range names {
		if name == active {
			continue
		}
		if err := m.broker.retire(ctx, m.key(name)); err != nil {
			continue
		}
		m.certs.Forget(name)
		m.mu.Lock()
		next := m.state
		next.Retire = nil
		for _, item := range m.state.Retire {
			if item != name {
				next.Retire = append(next.Retire, item)
			}
		}
		if m.save(ctx, next) == nil {
			m.state = next
		}
		m.mu.Unlock()
	}
}
