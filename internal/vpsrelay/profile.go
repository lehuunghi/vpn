// Package vpsrelay runs the VPS relay client inside VPN20.
package vpsrelay

import (
	"archive/zip"
	"bytes"
	"crypto/x509"
	"errors"
	"io"
	"net/netip"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const MaxProfileSize = 512 << 10

var (
	namePattern  = regexp.MustCompile("^[a-z0-9][a-z0-9-]{0,39}$")
	labelPattern = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
	keyPattern   = regexp.MustCompile("^[0-9a-f]{64}$")
)

func Domain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	parts := strings.Split(value, ".")
	if len(value) > 253 || len(parts) < 2 {
		return "", errors.New("Nhập domain relay, ví dụ relay.20.com.vn; không kèm https:// hoặc cổng.")
	}
	for _, part := range parts {
		if !labelPattern.MatchString(part) {
			return "", errors.New("Domain relay không hợp lệ.")
		}
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return "", errors.New("Dùng domain relay để xác minh chứng chỉ SSL.")
	}
	return value, nil
}

// profile is private persistent state. It must never appear in an API response.
type profile struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	ControlPort int    `json:"controlPort"`
	RemotePort  int    `json:"remotePort"`
	AccessKey   string `json:"accessKey"`
	Token       string `json:"token"`
	CA          string `json:"ca"`
}

func (p profile) validate() error {
	if _, err := Domain(p.Host); err != nil {
		return err
	}
	if !namePattern.MatchString(p.Name) || !keyPattern.MatchString(p.AccessKey) || !keyPattern.MatchString(p.Token) ||
		p.ControlPort < 1 || p.ControlPort > 65535 || p.RemotePort < 1 || p.RemotePort > 65535 {
		return errors.New("Profile relay thiếu khóa, tên hoặc cổng hợp lệ.")
	}
	if len(p.CA) == 0 || len(p.CA) > MaxProfileSize || !x509.NewCertPool().AppendCertsFromPEM([]byte(p.CA)) {
		return errors.New("Profile relay không có CA hợp lệ.")
	}
	return nil
}

func parseProfile(data []byte) (profile, error) {
	var p profile
	if len(data) == 0 || len(data) > MaxProfileSize {
		return p, errors.New("Gói profile relay phải là ZIP, tối đa 512 KB.")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) > 20 {
		return p, errors.New("Không đọc được ZIP profile relay.")
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		// Never extract files or execute scripts supplied by a profile.
		if strings.ContainsAny(f.Name, "/\\") || f.Name == "." || f.Name == ".." {
			return p, errors.New("ZIP profile relay chứa đường dẫn không hợp lệ.")
		}
		if f.Name != "frpc.toml" && f.Name != "ca.pem" {
			continue
		}
		limit := MaxProfileSize
		if f.Name == "frpc.toml" {
			limit = 32 << 10
		}
		if _, exists := files[f.Name]; exists || f.UncompressedSize64 > uint64(limit) {
			return p, errors.New("ZIP profile relay bị trùng tệp hoặc vượt kích thước.")
		}
		r, err := f.Open()
		if err != nil {
			return p, errors.New("Không đọc được tệp trong profile relay.")
		}
		body, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		_ = r.Close()
		if err != nil || len(body) > limit {
			return p, errors.New("Tệp trong profile relay không hợp lệ.")
		}
		files[f.Name] = body
	}
	var input struct {
		ServerAddr string
		ServerPort int
		User       string
		Metadatas  map[string]string
		Auth       struct{ Method, Token string }
		Transport  struct {
			TLS struct {
				Enable                    bool
				ServerName, TrustedCaFile string
			}
		}
		Proxies []struct {
			Name, Type string
			RemotePort int
		}
	}
	// Decode only data fields. Includes, plugins, local destinations, environment
	// templates and other frpc options are never applied to the embedded client.
	if _, err := toml.Decode(string(files["frpc.toml"]), &input); err != nil {
		return p, errors.New("frpc.toml trong profile relay không hợp lệ.")
	}
	host, err := Domain(input.ServerAddr)
	if err != nil {
		return p, err
	}
	tlsHost, err := Domain(input.Transport.TLS.ServerName)
	if err != nil || tlsHost != host || !input.Transport.TLS.Enable || input.Transport.TLS.TrustedCaFile != "ca.pem" ||
		input.Auth.Method != "token" || len(input.Proxies) != 1 || input.Proxies[0].Type != "udp" || input.Proxies[0].Name != "vpn20" {
		return p, errors.New("Cần profile UDP do lệnh vpn20-relay add/export tạo, có xác minh SSL.")
	}
	p = profile{Name: input.User, Host: host, ControlPort: input.ServerPort, RemotePort: input.Proxies[0].RemotePort,
		AccessKey: input.Metadatas["access_key"], Token: input.Auth.Token, CA: string(files["ca.pem"])}
	return p, p.validate()
}
