package ddns

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strings"
	"time"

	"github.com/mholt/acmez/v3/acme"
)

type sslError struct {
	code, message string
	cause         error
}

func (e *sslError) Error() string { return e.message + " [" + e.code + "]" }
func (e *sslError) Unwrap() error { return e.cause }

// Report actionable categories, never a raw CA response, account key, CSR,
// proxy password or an error's untrusted detail/HTML field.
func certificateMessage(err error) string {
	var known *sslError
	if errors.As(err, &known) {
		return known.Error()
	}
	var broker *apiError
	if errors.As(err, &broker) {
		return "Không tạo/xóa được TXT SSL: " + broker.Error()
	}
	var problem acme.Problem
	if errors.As(err, &problem) {
		messages := map[string]string{
			"caa":                "Bản ghi CAA không cho phép cấp chứng chỉ; kiểm tra quyền letsencrypt.org trên domain",
			"rateLimited":        "Máy chủ cấp chứng chỉ đang giới hạn lượt cấp; chờ theo hạn mức CA, tránh bấm cập nhật liên tục",
			"dns":                "CA không đọc được DNS xác thực; kiểm tra TXT, nameserver và DNSSEC của domain",
			"unauthorized":       "CA chưa xác minh được TXT; kiểm tra _acme-challenge trên DNS công khai",
			"rejectedIdentifier": "CA từ chối tên subdomain này; kiểm tra chính sách cấp chứng chỉ",
			"connection":         "CA không kết nối được tài nguyên xác thực; kiểm tra DNS công khai",
			"serverInternal":     "Máy chủ cấp chứng chỉ tạm lỗi; sẽ tự thử lại",
		}
		kind := strings.TrimPrefix(problem.Type, "urn:ietf:params:acme:error:")
		if message := messages[kind]; message != "" {
			return message + " [acme_" + kind + "]"
		}
		return "Máy chủ cấp chứng chỉ từ chối yêu cầu; kiểm tra log dịch vụ [acme_error]"
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return "Kết nối HTTPS tới CA không được tin cậy; kiểm tra chứng chỉ gốc và đồng hồ máy chủ [ca_tls]"
	}
	var lookup *net.DNSError
	if errors.As(err, &lookup) {
		return "Không phân giải được địa chỉ máy chủ cấp chứng chỉ; kiểm tra DNS hệ thống [ca_dns]"
	}
	var network net.Error
	if errors.As(err, &network) {
		return "Không kết nối được máy chủ cấp chứng chỉ; kiểm tra outbound HTTPS và proxy [ca_network]"
	}
	if errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), "storage") || strings.Contains(err.Error(), "permission denied") {
		return "Không đọc/ghi được kho chứng chỉ; kiểm tra quyền thư mục dữ liệu VPN20 [certificate_storage]"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Hết thời gian cấp chứng chỉ; kiểm tra kết nối CA và DNS [certificate_timeout]"
	}
	return "Chưa cấp được SSL; xem dòng automatic HTTPS certificate failed trong log dịch vụ [certificate_failed]"
}

func (m *Manager) certificateFailed(hostname string, err error) {
	message := certificateMessage(err)
	m.mu.Lock()
	m.certificateFailure = message
	m.certificateFailures++
	delay := 5 * time.Minute
	for i := 1; i < m.certificateFailures && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	m.retryAt = time.Now().Add(delay)
	m.mu.Unlock()
	if m.opts.Log != nil {
		m.opts.Log.Warn("automatic HTTPS certificate failed", "hostname", hostname, "error", err)
	}
	m.fail(fmt.Sprintf("%s · Tự thử lại sau %d phút", message, int(delay.Minutes())))
}
