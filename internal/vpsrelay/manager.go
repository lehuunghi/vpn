package vpsrelay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	frp "github.com/fatedier/frp/client"
	"github.com/fatedier/frp/client/proxy"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	frplog "github.com/fatedier/frp/pkg/util/log"
	goliblog "github.com/fatedier/golib/log"
)

const stateKey = "relay.v1"

func init() {
	// Remote login errors can contain arbitrary text. Report only our bounded,
	// translated statuses; never emit profile data or raw server replies.
	frplog.Logger = frplog.Logger.WithOptions(goliblog.WithOutput(io.Discard))
}

type Store interface {
	GetSetting(context.Context, string) (string, error)
	SetSetting(context.Context, string, string) error
}

type Options struct {
	Store     Store
	LocalPort int
	dial      func(context.Context, string, string) (net.Conn, error)
}

type savedState struct {
	Schema  int     `json:"schema"`
	Enabled bool    `json:"enabled"`
	Domain  string  `json:"domain"`
	Profile profile `json:"profile"`
}

type Status struct {
	Enabled     bool   `json:"enabled"`
	Configured  bool   `json:"configured"`
	Domain      string `json:"domain"`
	ProfileName string `json:"profileName,omitempty"`
	ControlPort int    `json:"controlPort,omitempty"`
	RemotePort  int    `json:"remotePort,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Phase       string `json:"phase"`
	Message     string `json:"message"`
}

type Input struct {
	Enabled    bool   `json:"enabled"`
	Domain     string `json:"domain"`
	ProfileZIP []byte `json:"profileZip,omitempty"`
}

type Manager struct {
	opts    Options
	opMu    sync.Mutex
	mu      sync.RWMutex
	state   savedState
	problem string
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	service *frp.Service
}

func New(opts Options) *Manager {
	m := &Manager{opts: opts, state: savedState{Schema: 1}}
	raw, err := opts.Store.GetSetting(context.Background(), stateKey)
	if err == nil && raw != "" {
		err = json.Unmarshal([]byte(raw), &m.state)
		if err == nil && (m.state.Schema != 1 || (m.state.Profile.Name != "" && m.state.Profile.validate() != nil)) {
			err = errors.New("invalid saved profile")
		}
		if err == nil && m.state.Enabled {
			_, err = Domain(m.state.Domain)
			if m.state.Profile.Name == "" {
				err = errors.New("missing profile")
			}
		}
	}
	if err != nil {
		m.problem = "Không đọc được cấu hình relay; nhập lại profile hoặc khôi phục bản sao lưu."
	}
	return m
}

func (m *Manager) Start(ctx context.Context) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stop()
	m.ctx = ctx
	m.restart()
}

func (m *Manager) Close() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stop()
	m.ctx = nil
}

func (m *Manager) Configure(ctx context.Context, input Input) (Status, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.RLock()
	next := m.state
	m.mu.RUnlock()
	if len(input.ProfileZIP) > 0 {
		p, err := parseProfile(input.ProfileZIP)
		if err != nil {
			return Status{}, err
		}
		next.Profile = p
		if strings.TrimSpace(input.Domain) == "" {
			input.Domain = p.Host
		}
	}
	if strings.TrimSpace(input.Domain) != "" {
		host, err := Domain(input.Domain)
		if err != nil {
			return Status{}, err
		}
		next.Domain = host
	}
	if input.Enabled {
		if err := next.Profile.validate(); err != nil {
			return Status{}, errors.New("Nhập ZIP profile do VPS cấp trước khi bật relay.")
		}
		if _, err := Domain(next.Domain); err != nil {
			return Status{}, err
		}
		if m.opts.LocalPort < 1 || m.opts.LocalPort > 65535 {
			return Status{}, errors.New("Không có cổng VPN hợp lệ để chạy relay.")
		}
	}
	next.Schema, next.Enabled = 1, input.Enabled
	data, err := json.Marshal(next)
	if err != nil {
		return Status{}, errors.New("Không lưu được cấu hình relay.")
	}
	if err = m.opts.Store.SetSetting(ctx, stateKey, string(data)); err != nil {
		return Status{}, errors.New("Không lưu được cấu hình relay.")
	}
	// Cancel the old runner before replacing credentials or its CA file.
	m.stop()
	m.mu.Lock()
	m.state, m.problem = next, ""
	m.mu.Unlock()
	m.restart()
	return m.Snapshot(), nil
}

// Endpoint remains the selected relay endpoint while reconnecting. A relay
// outage must not silently replace downloaded configs with a direct endpoint.
func (m *Manager) Endpoint() (string, int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.Domain, m.state.Profile.RemotePort, m.state.Enabled && m.state.Profile.Name != ""
}

func (m *Manager) Snapshot() Status {
	m.mu.RLock()
	s, problem, service := m.state, m.problem, m.service
	m.mu.RUnlock()
	out := Status{Enabled: s.Enabled, Configured: s.Profile.Name != "", Domain: s.Domain,
		ProfileName: s.Profile.Name, RemotePort: s.Profile.RemotePort, ControlPort: s.Profile.ControlPort,
		Phase: "disabled", Message: "Kết nối trực tiếp."}
	if out.Configured {
		out.Endpoint = net.JoinHostPort(s.Domain, strconv.Itoa(s.Profile.RemotePort))
	}
	if !s.Enabled {
		if problem != "" {
			out.Phase, out.Message = "error", problem
		}
		return out
	}
	out.Phase, out.Message = "connecting", "Đang kết nối relay; VPN20 tự thử lại khi mất mạng."
	if problem != "" {
		out.Phase, out.Message = "error", problem
	}
	if service != nil {
		if status, ok := service.StatusExporter().GetProxyStatus("vpn20"); ok {
			switch status.Phase {
			case proxy.ProxyPhaseRunning:
				out.Phase, out.Message = "connected", "Đã kết nối relay. Tải lại cấu hình hoặc quét lại QR trên thiết bị."
			case proxy.ProxyPhaseStartErr:
				out.Phase, out.Message = "error", "Relay chưa mở được cổng UDP; kiểm tra profile, cổng được cấp và máy khác đang dùng cùng profile."
			}
		}
	}
	return out
}

// stop/restart are serialized by opMu. Each old runner exits before a new one
// can claim the same profile, including after repeated saves or service updates.
func (m *Manager) stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.RLock()
	service := m.service
	m.mu.RUnlock()
	if service != nil {
		service.Close()
	}
	if m.done != nil {
		<-m.done
	}
	m.cancel, m.done = nil, nil
}

func (m *Manager) restart() {
	m.mu.RLock()
	s, problem := m.state, m.problem
	m.mu.RUnlock()
	if m.ctx == nil || !s.Enabled || problem != "" {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel, m.done = cancel, make(chan struct{})
	go m.run(ctx, s, m.done)
}

func (m *Manager) run(ctx context.Context, s savedState, done chan struct{}) {
	defer close(done)
	for ctx.Err() == nil {
		attemptCtx, endAttempt := context.WithCancel(ctx)
		service, err := m.newService(s, endAttempt)
		if err == nil {
			m.mu.Lock()
			m.service = service
			m.mu.Unlock()
			err = service.Run(attemptCtx)
			service.Close()
			m.mu.Lock()
			m.service = nil
			m.mu.Unlock()
		}
		endAttempt()
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		m.problem = safeError(err)
		m.mu.Unlock()
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func safeError(err error) string {
	if err == nil {
		return "Kết nối relay bị ngắt; VPN20 đang tự kết nối lại."
	}
	text := ""
	if err != nil {
		text = strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(text, "x509") || strings.Contains(text, "certificate") || strings.Contains(text, "tls"):
		return "Chứng chỉ SSL của relay không hợp lệ; kiểm tra domain, thời gian hệ thống và profile CA."
	case strings.Contains(text, "lookup") || strings.Contains(text, "no such host"):
		return "Không phân giải được domain relay; kiểm tra DNS và Internet."
	case strings.Contains(text, "dial") || strings.Contains(text, "connect") || strings.Contains(text, "timeout"):
		return "Không kết nối được relay; kiểm tra Internet, domain và cổng TCP trên VPS."
	case strings.Contains(text, "permission") || strings.Contains(text, "read-only") || strings.Contains(text, "mkdir"):
		return "Không lưu được CA relay; kiểm tra quyền thư mục dữ liệu VPN20."
	default:
		return "Relay từ chối kết nối; kiểm tra profile còn hiệu lực và chỉ dùng cho một máy VPN20."
	}
}

func (m *Manager) newService(s savedState, disconnected context.CancelFunc) (*frp.Service, error) {
	common := &v1.ClientCommonConfig{
		ServerAddr: s.Domain, ServerPort: s.Profile.ControlPort, User: s.Profile.Name, LoginFailExit: new(true),
		Metadatas: map[string]string{"access_key": s.Profile.AccessKey},
		Auth:      v1.AuthClientConfig{Method: "token", Token: s.Profile.Token, AdditionalScopes: []v1.AuthScope{"HeartBeats", "NewWorkConns"}},
		Transport: v1.ClientTransportConfig{Protocol: "tcp", DialServerTimeout: 5, HeartbeatInterval: 10, HeartbeatTimeout: 30,
			TLS: v1.TLSClientConfig{Enable: new(true), TLSConfig: v1.TLSConfig{ServerName: s.Domain}}},
	}
	p := &v1.UDPProxyConfig{ProxyBaseConfig: v1.ProxyBaseConfig{Name: "vpn20", Type: "udp",
		ProxyBackend: v1.ProxyBackend{LocalIP: "127.0.0.1", LocalPort: m.opts.LocalPort},
		Transport:    v1.ProxyTransport{BandwidthLimitMode: "server"}}, RemotePort: s.Profile.RemotePort}
	src := source.NewConfigSource()
	if err := src.ReplaceAll([]v1.ProxyConfigurer{p}, nil); err != nil {
		return nil, err
	}
	return frp.NewService(frp.ServiceOptions{Common: common, ConfigSourceAggregator: source.NewAggregator(src),
		ConnectorCreator: func(ctx context.Context, cfg *v1.ClientCommonConfig) frp.Connector {
			return &tcpConnector{ctx: ctx, cfg: cfg, ca: s.Profile.CA, dial: m.opts.dial, onClose: disconnected}
		}})
}
