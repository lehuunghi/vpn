package ddns

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Never include transport error text, headers or response bodies: they can
// contain credentials or HTML from a proxy/Cloudflare Access login page.
func (b *broker) location(method, path string) string {
	u, _ := url.Parse(b.base)
	return fmt.Sprintf("%s %s, %s", method, path, u.Host)
}

func (b *broker) connectionError(err error, method, path string) error {
	message := "Không mở được kết nối tới API DDNS; kiểm tra Internet, firewall và Custom Domain của Worker"
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var network net.Error
	switch {
	case errors.As(err, &dns):
		message = "Không phân giải được tên API DDNS; kiểm tra DNS và Custom Domain của Worker"
	case errors.As(err, &certificate):
		message = "Chứng chỉ HTTPS của API DDNS không hợp lệ; kiểm tra SSL và đồng hồ máy chủ"
	case errors.Is(err, context.Canceled):
		message = "Yêu cầu DDNS đã bị hủy; sẽ thử lại khi dịch vụ chạy"
	case errors.As(err, &network) && network.Timeout():
		message = "Kết nối API DDNS quá thời gian chờ; kiểm tra đường truyền và trạng thái Worker"
	}
	return fmt.Errorf("%s (%s)", message, b.location(method, path))
}

func (b *broker) responseError(status int, code, method, path string) error {
	messages := map[string]string{
		"name_taken":            "Tên subdomain đã được sử dụng",
		"claim_limit":           "Mạng này đã đạt hạn mức đăng ký trong ngày",
		"capacity":              "Dịch vụ đã đạt hạn mức hostname",
		"unauthorized":          "Khóa máy không hợp lệ hoặc đã bị thu hồi",
		"registration_disabled": "Dịch vụ đã tắt đăng ký tự động",
		"dns_record_conflict":   "Tên có bản ghi DNS ngoài quyền quản lý; cần quản trị viên kiểm tra",
		"dns_api_error":         "Worker không cập nhật được Cloudflare DNS; kiểm tra CF_ZONE_ID, quyền token và hạn mức",
		"dns_auth_error":        "Cloudflare từ chối token của Worker; kiểm tra CF_API_TOKEN và quyền DNS Write cho zone 20.com.vn",
		"dns_zone_error":        "Worker không tìm thấy zone Cloudflare; kiểm tra CF_ZONE_ID và phạm vi token",
		"dns_api_unavailable":   "Worker không kết nối được API Cloudflare; sẽ tự thử lại",
		"not_configured":        "Worker thiếu cấu hình; kiểm tra binding DB, CF_ZONE_ID và Secrets CF_API_TOKEN, ADMIN_TOKEN rồi Deploy lại",
		"schema_outdated":       "D1 thiếu bảng hoặc cột; chạy schema.sql mới trên database đang gắn binding DB",
		"database_unavailable":  "Worker không truy cập được D1; kiểm tra binding DB, trạng thái và hạn mức D1",
		"service_error":         "Worker xử lý lỗi; nâng Worker lên 0.2.1, mở /health và kiểm tra log Worker",
		"rate_limited":          "Dịch vụ đang bận; sẽ tự thử lại",
		"acme_busy":             "Dịch vụ đang xác thực SSL; sẽ tự thử lại",
		"acme_limit":            "Đã đạt hạn mức xác thực SSL; sẽ tự thử lại",
		"busy":                  "Dịch vụ đang bận; sẽ tự thử lại",
	}
	message, known := messages[code]
	if !known {
		code = "http_error"
		switch {
		case status == 404 || status == 405:
			code = "worker_upgrade"
			message = "API chưa hỗ trợ đăng ký máy/SSL; kiểm tra địa chỉ API và Deploy Worker mới"
		case status >= 300 && status < 400, status == 401 || status == 403:
			message = "API bị chuyển hướng hoặc chặn truy cập; kiểm tra Cloudflare Access/WAF và Custom Domain của Worker"
		case status == 429:
			message = "API bị giới hạn lượt gọi; kiểm tra hạn mức Worker/Cloudflare"
		default:
			message = "API DDNS trả lỗi HTTP; kiểm tra /health và log Worker"
		}
	}
	return &apiError{status, code, fmt.Sprintf("%s (HTTP %d, %s; %s)", message, status, code, b.location(method, path))}
}
