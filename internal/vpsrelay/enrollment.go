package vpsrelay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Enrollment credentials are sent only after hostname/CA verification. The
// password is never persisted; a private claim makes lost responses retryable.
func (m *Manager) enroll(ctx context.Context, host string, port int, password string) (profile, error) {
	var p profile
	if len([]rune(password)) < 12 || len([]rune(password)) > 256 {
		return p, errors.New("Mật khẩu relay cần từ 12 đến 256 ký tự.")
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	id := sha256.Sum256([]byte(address))
	key := "relay.claim." + hex.EncodeToString(id[:])
	claim, err := m.opts.Store.GetSetting(ctx, key)
	if err != nil {
		return p, errors.New("Không đọc được khóa đăng ký relay.")
	}
	if !keyPattern.MatchString(claim) {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return p, errors.New("Không tạo được khóa đăng ký relay.")
		}
		claim = hex.EncodeToString(raw[:])
		if err := m.opts.Store.SetSetting(ctx, key, claim); err != nil {
			return p, errors.New("Không lưu được khóa đăng ký relay.")
		}
	}
	body, _ := json.Marshal(map[string]string{"password": password, "claim": claim})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+address+"/api/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return p, errors.New("Domain relay không hợp lệ.")
	}
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12,
		RootCAs: m.opts.enrollRoots, NextProtos: []string{"http/1.1"}},
		ForceAttemptHTTP2: false, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	if m.opts.dial != nil {
		transport.DialContext = m.opts.dial
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return p, errors.New(safeError(err))
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return p, errors.New("Đã thử đăng ký relay quá nhiều lần; chờ một phút rồi thử lại.")
	case http.StatusForbidden, http.StatusUnauthorized:
		return p, errors.New("Mật khẩu relay không đúng, máy đã bị thu hồi hoặc VPS đã hết cổng.")
	default:
		return p, errors.New("VPS chưa hỗ trợ đăng ký bằng mật khẩu; cập nhật bộ cài relay lên 1.1.0.")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(data) > 16384 || json.Unmarshal(data, &p) != nil || p.Host != host || p.ControlPort != port || p.CA != "" {
		return profile{}, errors.New("VPS trả về cấu hình relay không hợp lệ.")
	}
	p.SystemTrust = true
	if err := p.validate(); err != nil {
		return profile{}, errors.New("VPS trả về cấu hình relay không hợp lệ.")
	}
	return p, nil
}
