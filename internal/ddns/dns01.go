package ddns

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/mholt/acmez/v3"
	"github.com/mholt/acmez/v3/acme"
	"github.com/miekg/dns"
)

// The broker already knows the exact Cloudflare zone and owns only this
// machine's hostname. SOA/NS discovery on the VPN server is unnecessary.
// Public TXT checks use RFC 8484 HTTPS instead of requiring direct port 53.
type brokerSolver struct {
	manager           *Manager
	dns               *txtChecker
	timeout, interval time.Duration
}

func newBrokerSolver(m *Manager) *brokerSolver {
	return &brokerSolver{manager: m, timeout: 2 * time.Minute, interval: 3 * time.Second,
		dns: &txtChecker{client: &http.Client{Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
			endpoints: []string{"https://cloudflare-dns.com/dns-query", "https://dns.google/dns-query"}}}
}

func (s *brokerSolver) key(ch acme.Challenge) (string, error) {
	host := ch.Identifier.Value
	if ch.Type != "dns-01" || ch.Identifier.Type != "dns" || ch.KeyAuthorization == "" || host != strings.ToLower(host) {
		return "", errors.New("Yêu cầu xác thực SSL không hợp lệ")
	}
	if key := s.manager.key(host); key != "" {
		return key, nil
	}
	return "", errors.New("Hostname nằm ngoài quyền của máy")
}

func (s *brokerSolver) Present(ctx context.Context, ch acme.Challenge) error {
	key, err := s.key(ch)
	if err != nil {
		return err
	}
	s.manager.sslProgress("Đang tạo TXT xác thực SSL…")
	return s.manager.broker.challenge(ctx, key, ch.DNS01KeyAuthorization(), false)
}

func (s *brokerSolver) Wait(ctx context.Context, ch acme.Challenge) error {
	if _, err := s.key(ch); err != nil {
		return err
	}
	s.manager.sslProgress("Đang chờ TXT xác thực xuất hiện trên DNS công khai…")
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	var last error
	for {
		ready, err := s.dns.visible(ctx, ch.DNS01TXTRecordName(), ch.DNS01KeyAuthorization())
		if ready {
			s.manager.sslProgress("TXT đã sẵn sàng; đang chờ máy chủ cấp chứng chỉ…")
			return nil
		}
		// Keep a completed lookup's result if cancellation interrupted the last
		// HTTP request; it tells a missing record apart from a network outage.
		if ctx.Err() == nil {
			last = err
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			if last != nil {
				return last
			}
			return &sslError{"dns_txt_not_visible", "TXT xác thực chưa xuất hiện trên DNS công khai; kiểm tra nameserver của 20.com.vn và bản ghi _acme-challenge", ctx.Err()}
		case <-ticker.C:
		}
	}
}

func (s *brokerSolver) CleanUp(ctx context.Context, ch acme.Challenge) error {
	key, err := s.key(ch)
	if err != nil {
		return err
	}
	// ACME cleanup can follow cancellation. Remove only this challenge value
	// with a fresh bounded context, rather than leaving TXT behind on retry.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.manager.broker.challenge(ctx, key, ch.DNS01KeyAuthorization(), true)
}

func (m *Manager) sslProgress(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Busy && m.status.Phase == "ssl" {
		m.status.Message = message
	}
}

type txtChecker struct {
	client    *http.Client
	endpoints []string
}

func (d *txtChecker) visible(ctx context.Context, name, value string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		ready bool
		err   error
	}
	results := make(chan answer, len(d.endpoints))
	for _, endpoint := range d.endpoints {
		go func(endpoint string) {
			ready, err := d.lookup(ctx, endpoint, name, value)
			results <- answer{ready, err}
		}(endpoint)
	}
	var failure error
	completed := false
	for range d.endpoints {
		r := <-results
		if r.ready {
			return true, nil
		}
		if r.err == nil {
			completed = true
		} else {
			failure = r.err
		}
	}
	if completed {
		return false, nil
	}
	if failure == nil {
		failure = &sslError{"dns_doh_unavailable", "Không có dịch vụ kiểm tra TXT qua HTTPS", nil}
	}
	return false, failure
}

func (d *txtChecker) lookup(ctx context.Context, endpoint, name, value string) (bool, error) {
	query := new(dns.Msg).SetQuestion(dns.Fqdn(name), dns.TypeTXT)
	query.Id = 0 // RFC 8484: maximize cache friendliness.
	wire, _ := query.Pack()
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(wire))
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("Content-Type", "application/dns-message")
	response, err := d.client.Do(req)
	if err != nil {
		return false, &sslError{"dns_doh_unavailable", "Không kết nối được dịch vụ kiểm tra TXT; cho phép HTTPS tới cloudflare-dns.com hoặc dns.google", err}
	}
	defer response.Body.Close()
	kind, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	wire, err = io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(wire) > 65536 || response.StatusCode != 200 || kind != "application/dns-message" {
		return false, &sslError{"dns_doh_unavailable", "Dịch vụ kiểm tra TXT trả phản hồi không hợp lệ; kiểm tra proxy/firewall HTTPS", err}
	}
	var result dns.Msg
	if result.Unpack(wire) != nil || !result.Response || result.Truncated || result.Id != query.Id || len(result.Question) != 1 || result.Question[0] != query.Question[0] {
		return false, &sslError{"dns_doh_invalid", "Phản hồi kiểm tra TXT không đúng yêu cầu DNS", nil}
	}
	if result.Rcode == dns.RcodeNameError {
		return false, nil
	}
	if result.Rcode != dns.RcodeSuccess {
		return false, &sslError{"dns_servfail", "DNS công khai chưa phân giải được TXT; kiểm tra nameserver và DNSSEC của 20.com.vn", nil}
	}
	for _, record := range result.Answer {
		if txt, ok := record.(*dns.TXT); ok && strings.EqualFold(txt.Hdr.Name, query.Question[0].Name) && strings.Join(txt.Txt, "") == value {
			return true, nil
		}
	}
	return false, nil
}

var _ acmez.Solver = (*brokerSolver)(nil)
var _ acmez.Waiter = (*brokerSolver)(nil)
