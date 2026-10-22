package publicaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

// Neither provider error bodies nor transport errors are returned: they may
// contain credentials, request URLs or proxy information.
func (m *Manager) api(ctx context.Context, method, path string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, m.apiURL+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("Không tạo được yêu cầu Cloudflare")
	}
	req.Header.Set("Authorization", "Bearer "+m.opt.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return errors.New("Không kết nối được Cloudflare; kiểm tra mạng và thử lại")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cloudflare trả HTTP %d; kiểm tra Zone ID, API token và quyền Zone DNS Write", resp.StatusCode)
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Info    struct {
			TotalCount int `json:"total_count"`
		} `json:"result_info"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil || !envelope.Success {
		return errors.New("Cloudflare không xác nhận yêu cầu DNS; kiểm tra quyền của API token")
	}
	if envelope.Info.TotalCount > 100 {
		return errors.New("Tên đã có quá nhiều bản ghi DNS; chọn subdomain khác")
	}
	if out != nil {
		if err = json.Unmarshal(envelope.Result, out); err != nil {
			return errors.New("Phản hồi DNS Cloudflare không hợp lệ")
		}
	}
	return nil
}

func (m *Manager) records(ctx context.Context) ([]record, error) {
	var records []record
	query := url.Values{"name": {m.state.Subdomain + "." + m.state.Domain}, "per_page": {"100"}}
	err := m.api(ctx, http.MethodGet, "/zones/"+m.opt.ZoneID+"/dns_records?"+query.Encode(), nil, &records)
	return records, err
}

func (m *Manager) owned(r record) bool {
	return idPattern.MatchString(r.ID) && r.Type == "A" && r.Name == m.state.Subdomain+"."+m.state.Domain && r.Comment == "VPN20:"+m.state.Owner
}

func (m *Manager) syncRecord(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ip := m.state.IPv4
	if m.state.AutoIPv4 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.ipURL, nil)
		resp, err := m.client.Do(req)
		if err != nil {
			return errors.New("Không xác định được IPv4 công khai; thử nhập IPv4 thủ công")
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			return errors.New("Dịch vụ kiểm tra IPv4 không sẵn sàng; thử nhập IPv4 thủ công")
		}
		ip = strings.TrimSpace(string(b))
	}
	if !publicIPv4(ip) {
		return errors.New("Không có IPv4 công khai hợp lệ; kiểm tra IP mạng hoặc dùng Cloudflare Tunnel cho trang quản trị")
	}
	records, err := m.records(ctx)
	if err != nil {
		return err
	}
	if len(records) > 1 || (len(records) == 1 && !m.owned(records[0])) {
		return errors.New("Subdomain đã có bản ghi khác. Chọn tên khác; VPN20 không ghi đè DNS có sẵn")
	}
	r := record{Type: "A", Name: m.state.Subdomain + "." + m.state.Domain, Content: ip, TTL: 300, Comment: "VPN20:" + m.state.Owner}
	var result record
	path := "/zones/" + m.opt.ZoneID + "/dns_records"
	switch {
	case len(records) == 0:
		err = m.api(ctx, http.MethodPost, path, r, &result)
	case records[0].Content != ip || records[0].Proxied || records[0].TTL != 300:
		err = m.api(ctx, http.MethodPut, path+"/"+records[0].ID, r, &result)
	default:
		result = records[0]
	}
	if err != nil {
		return err
	}
	if !m.owned(result) || result.Content != ip || result.Proxied {
		return errors.New("Cloudflare chưa xác nhận đúng bản ghi DNS-only đã yêu cầu")
	}
	m.state.RecordID = result.ID
	m.state.LastIP = ip
	now := time.Now().UTC()
	m.state.LastSync = &now
	if err = m.persist(ctx); err != nil {
		return err
	}
	m.lastError = ""
	return nil
}
