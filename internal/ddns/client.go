// Package ddns integrates the VPN20 DDNS and DNS-01 broker without distributing
// Cloudflare or central administrator credentials to installed servers.
package ddns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const Domain = "vpn.20.com.vn"
const DefaultAPI = "https://ddns.20.com.vn"

type apiError struct {
	Status        int
	Code, Message string
}

func (e *apiError) Error() string { return e.Message }

type broker struct {
	base           string
	normal, v4, v6 *http.Client
}

func newBroker(base string) (*broker, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return nil, errors.New("Địa chỉ DDNS phải dùng HTTPS")
	}
	b := &broker{base: strings.TrimRight(base, "/")}
	for network, target := range map[string]**http.Client{"tcp": &b.normal, "tcp4": &b.v4, "tcp6": &b.v6} {
		network := network
		dialer := &net.Dialer{Timeout: 8 * time.Second}
		// No HTTP_PROXY: the source IP must be the server's own Internet egress.
		transport := &http.Transport{TLSHandshakeTimeout: 8 * time.Second, IdleConnTimeout: 60 * time.Second,
			DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, address)
			}}
		// A paired update or DNS-01 cleanup makes several Cloudflare requests.
		// Do not abandon it after 15s while the Worker is still processing it.
		*target = &http.Client{Transport: transport, Timeout: 60 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return b, nil
}
func isLoopback(host string) bool {
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

func (b *broker) call(ctx context.Context, client *http.Client, method, path, token string, input, output any) error {
	var data []byte
	var err error
	if input != nil {
		data, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("Không tạo được yêu cầu DDNS")
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return b.connectionError(err, method, path)
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return errors.New("Không đọc được phản hồi DDNS")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var result struct{ Error string }
		_ = json.Unmarshal(data, &result)
		// Only forward the known broker error vocabulary, never arbitrary HTML/body.
		return b.responseError(response.StatusCode, result.Error, method, path)
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return fmt.Errorf("API DDNS không trả JSON hợp lệ; kiểm tra Custom Domain, Cloudflare Access/WAF (%s)", b.location(method, path))
	}
	return nil
}
func (b *broker) claim(ctx context.Context, label, token string) error {
	var result struct {
		OK       bool
		Hostname string
	}
	err := b.call(ctx, b.normal, "POST", "/v1/claim", token, map[string]string{"name": label}, &result)
	if err == nil && (!result.OK || result.Hostname != label+"."+Domain) {
		return errors.New("Dịch vụ cấp hostname không đúng yêu cầu")
	}
	return err
}
func (b *broker) sync(ctx context.Context, token string) (string, string, error) {
	type answer struct{ IP, Type string }
	type outcome struct {
		family int
		value  answer
		err    error
	}
	replies := make(chan outcome, 2)
	for family, client := range map[int]*http.Client{4: b.v4, 6: b.v6} {
		go func(family int, client *http.Client) {
			var a answer
			err := b.call(ctx, client, "GET", "/v1/ip", "", nil, &a)
			address, parseErr := netip.ParseAddr(a.IP)
			if err == nil && (parseErr != nil || !address.IsGlobalUnicast() || address.IsPrivate() || (family == 4) != address.Is4()) {
				err = errors.New("IP phản hồi không hợp lệ")
			}
			replies <- outcome{family, a, err}
		}(family, client)
	}
	addresses := map[int]string{}
	failures := map[int]error{}
	for range 2 {
		r := <-replies
		if r.err == nil {
			addresses[r.family] = r.value.IP
		} else {
			failures[r.family] = r.err
		}
	}
	if len(addresses) == 0 {
		return "", "", fmt.Errorf("Không xác định được IP Internet. IPv4: %v; IPv6: %v", failures[4], failures[6])
	}
	input := map[string]any{}
	for family, field := range map[int]string{4: "ipv4", 6: "ipv6"} {
		if ip := addresses[family]; ip != "" {
			input[field] = ip
		} else {
			input[field] = nil
		}
	}
	var result struct{ OK bool }
	if err := b.call(ctx, b.normal, "POST", "/v1/update", token, input, &result); err != nil {
		return "", "", err
	}
	if !result.OK {
		return "", "", errors.New("Dịch vụ chưa xác nhận cập nhật DNS")
	}
	return addresses[4], addresses[6], nil
}
func (b *broker) challenge(ctx context.Context, token, value string, remove bool) error {
	path := "/v1/acme/present"
	if remove {
		path = "/v1/acme/cleanup"
	}
	return b.call(ctx, b.normal, "POST", path, token, map[string]string{"value": value}, nil)
}
func (b *broker) retire(ctx context.Context, token string) error {
	return b.call(ctx, b.normal, "POST", "/v1/retire", token, struct{}{}, nil)
}
func (b *broker) close() {
	for _, c := range []*http.Client{b.normal, b.v4, b.v6} {
		c.CloseIdleConnections()
	}
}
