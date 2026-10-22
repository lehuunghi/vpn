# VPN20 1.2.1

- Đổi HTTPS tự động mặc định sang TCP 8443. Link quản trị là https://ten.vpn.20.com.vn:8443; giữ HTTP LAN ở 51821 và cổng UDP WireGuard hiện có.
- Sửa lỗi DDNS bị che bởi thông báo chung: phân biệt DNS, SSL của API, timeout, cấu hình Worker, schema/quota D1, token Cloudflare và Access/WAF. Hiển thị bước API/HTTP status, không đưa token, HTML hay lỗi SQL ra giao diện.
- Kèm Worker 0.2.1: tự bổ sung bảng phụ khi nâng D1 từ 0.1.0, health kiểm tra schema thực tế; giữ hostname/khóa/dữ liệu. Database mới vẫn cần schema.sql.
- Tăng thời gian chờ API để tránh ngắt cập nhật IPv4/IPv6 và DNS-01 đang xử lý; tăng thời hạn khóa thao tác Worker tương ứng.
- Cập nhật mapping Docker và hướng dẫn Windows/Linux/Docker cho 8443.

**Cần Deploy Worker 0.2.1 tại ddns.20.com.vn để nhận đầy đủ bản sửa.**
Thay src/index.js, giữ DB/Variables/Secrets hiện có. Sao lưu D1; chạy schema.sql nếu health báo schema_outdated. Health cần trả ok: true, version: 0.2.1.
Không đưa CF_API_TOKEN hay ADMIN_TOKEN vào VPN20. API trung tâm vẫn dùng HTTPS 443.

Sau khi nâng VPN20: chuyển tiếp TCP 8443 về máy chủ, mở firewall 8443. Nếu đã tự đặt VPN20_DDNS_HTTPS=:443, đổi thành :8443 hoặc bỏ biến rồi restart. Container cũ cần mapping 8443:8443 và tạo lại container. Hostname/khóa/chứng chỉ được giữ; DNS/SSL không vượt CGNAT hay tự cấu hình router.

Tải bộ cài x64 cho Windows Intel/AMD 64-bit hoặc x86 cho Windows 32-bit. Không cung cấp Windows ARM64. Có gói Linux amd64/arm64 và Docker.

Ứng dụng/bộ cài chưa ký mã; Windows có thể hiện Unknown publisher. Driver Wintun/WireGuardNT giữ chữ ký chính hãng. Đối chiếu SHA-256 trong Assets.

Hướng dẫn: [subdomain/SSL](https://github.com/lehuunghi/vpn/blob/main/docs/subdomain.md) · [cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md).
