// Package publicaccess manages one Cloudflare DNS-only A record per server.
// DNS configuration does not imply that a router, firewall or HTTPS proxy is ready.
package publicaccess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"
)

const settingsKey = "cloudflare_public_access"

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Store interface {
	GetSetting(context.Context, string) (string, error)
	SetSetting(context.Context, string, string) error
}

type Options struct{ Domain, ZoneID, Token string }

type Input struct {
	Subdomain string `json:"subdomain"`
	AutoIPv4  bool   `json:"autoIPv4"`
	IPv4      string `json:"ipv4"`
}

type saved struct {
	Input
	Domain   string     `json:"domain"`
	ZoneID   string     `json:"zoneID"`
	Owner    string     `json:"owner"`
	RecordID string     `json:"recordID"`
	LastIP   string     `json:"lastIP"`
	LastSync *time.Time `json:"lastSync,omitempty"`
}

type Status struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Domain    string `json:"domain"`
	Input
	Hostname   string     `json:"hostname,omitempty"`
	Registered bool       `json:"registered"`
	LastIP     string     `json:"lastIP,omitempty"`
	LastSync   *time.Time `json:"lastSync,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type Manager struct {
	mu                   sync.Mutex
	opt                  Options
	st                   Store
	state                saved
	loadError, lastError string
	client               *http.Client
	apiURL, ipURL        string
}

func New(opt Options, st Store) *Manager {
	m := &Manager{opt: opt, st: st, apiURL: "https://api.cloudflare.com/client/v4", ipURL: "https://api4.ipify.org",
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	m.state.AutoIPv4 = true
	raw, err := st.GetSetting(context.Background(), settingsKey)
	if err != nil || (raw != "" && json.Unmarshal([]byte(raw), &m.state) != nil) {
		m.loadError = "Không đọc được cài đặt subdomain đã lưu. Khôi phục bản sao lưu trước khi tiếp tục."
	}
	return m
}

func validDomain(domain string) bool {
	if len(domain) > 190 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	_, err := netip.ParseAddr(domain)
	return err != nil
}

func (m *Manager) reason() string {
	if m.loadError != "" {
		return m.loadError
	}
	if !validDomain(m.opt.Domain) || !idPattern.MatchString(m.opt.ZoneID) || m.opt.Token == "" {
		return "Cấu hình domain, Zone ID và API token Cloudflare trên máy chủ để bật chức năng này."
	}
	if m.state.Subdomain != "" && (m.state.Domain != m.opt.Domain || m.state.ZoneID != m.opt.ZoneID || !idPattern.MatchString(m.state.Owner) || !labelPattern.MatchString(m.state.Subdomain)) {
		return "Domain/zone đã thay đổi hoặc cài đặt cũ không hợp lệ. Khôi phục cấu hình cũ, gỡ subdomain rồi đổi domain."
	}
	return ""
}

func (m *Manager) status() Status {
	r := m.reason()
	s := Status{Available: r == "", Reason: r, Domain: m.opt.Domain, Input: m.state.Input, Registered: m.state.RecordID != "", LastIP: m.state.LastIP, LastSync: m.state.LastSync, Error: m.lastError}
	if m.state.Subdomain != "" {
		s.Hostname = m.state.Subdomain + "." + m.state.Domain
	}
	return s
}

func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status() }

func (m *Manager) persist(ctx context.Context) error {
	b, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	if err = m.st.SetSetting(ctx, settingsKey, string(b)); err != nil {
		return errors.New("Không lưu được cài đặt subdomain")
	}
	return nil
}

func (m *Manager) Save(ctx context.Context, in Input) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.reason(); r != "" {
		return m.status(), errors.New(r)
	}
	in.Subdomain = strings.ToLower(strings.TrimSpace(in.Subdomain))
	in.IPv4 = strings.TrimSpace(in.IPv4)
	if !labelPattern.MatchString(in.Subdomain) {
		return m.status(), errors.New("Subdomain gồm 1–63 chữ cái a-z, số hoặc dấu gạch ngang; không nhập domain đầy đủ")
	}
	if !in.AutoIPv4 && !publicIPv4(in.IPv4) {
		return m.status(), errors.New("Cần địa chỉ IPv4 công khai hợp lệ; IP LAN và CGNAT không dùng được")
	}
	if in.AutoIPv4 {
		in.IPv4 = ""
	}
	if m.state.Subdomain != "" && m.state.Subdomain != in.Subdomain {
		return m.status(), errors.New("Gỡ subdomain hiện tại trước khi chọn tên mới")
	}
	old := m.state
	if m.state.Owner == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return m.status(), err
		}
		m.state.Owner = hex.EncodeToString(b)
	}
	m.state.Input = in
	m.state.Domain = m.opt.Domain
	m.state.ZoneID = m.opt.ZoneID
	// Persist ownership before calling Cloudflare. A timed-out create can be
	// recovered by its comment after restart, without adopting someone else's DNS.
	if err := m.persist(ctx); err != nil {
		m.state = old
		return m.status(), err
	}
	err := m.syncRecord(ctx)
	if err != nil {
		m.lastError = err.Error()
	}
	return m.status(), err
}

func (m *Manager) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Subdomain == "" {
		return nil
	}
	if r := m.reason(); r != "" {
		return errors.New(r)
	}
	err := m.syncRecord(ctx)
	if err != nil {
		m.lastError = err.Error()
	}
	return err
}

func (m *Manager) Run(ctx context.Context) {
	_ = m.Sync(ctx)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Sync(ctx)
		}
	}
}

func (m *Manager) Delete(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.reason(); r != "" {
		return m.status(), errors.New(r)
	}
	if m.state.Subdomain == "" {
		return m.status(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	records, err := m.records(ctx)
	if err != nil {
		return m.status(), err
	}
	for _, record := range records {
		if m.owned(record) {
			if err = m.api(ctx, http.MethodDelete, "/zones/"+m.opt.ZoneID+"/dns_records/"+record.ID, nil, nil); err != nil {
				return m.status(), err
			}
		}
	}
	old := m.state
	m.state = saved{Input: Input{AutoIPv4: true}, Owner: old.Owner}
	if err = m.persist(ctx); err != nil {
		m.state = old
		return m.status(), err
	}
	m.lastError = ""
	return m.status(), nil
}

func publicIPv4(raw string) bool {
	a, err := netip.ParseAddr(raw)
	if err != nil || !a.Is4() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
		if netip.MustParsePrefix(p).Contains(a) {
			return false
		}
	}
	return true
}
