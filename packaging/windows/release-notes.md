# VPN20 1.2.0

- Cài đặt thêm một ô tên subdomain .vpn.20.com.vn, nút Cập nhật và một dòng trạng thái.
- Tự đăng ký hostname, đồng bộ IPv4/IPv6 và cấp/gia hạn SSL qua CertMagic của Caddy; HTTPS mặc định TCP 443, không tự chuyển cổng khi bị chiếm.
- Duy trì HTTP qua IP LAN; Windows tự thêm firewall riêng cho HTTPS khi bật tính năng. Cookie HTTPS có Secure.
- Giữ tên/khóa và chứng chỉ khi khởi động lại hoặc nâng cấp. Đổi tên chỉ thay Endpoint sau khi DNS/SSL sẵn sàng; gỡ DNS do dịch vụ quản lý ở tên cũ và giữ quyền sở hữu.
- Kèm Worker DDNS 0.2.0, migration D1 không xóa dữ liệu, khóa riêng từng tên, TXT DNS-01 giới hạn theo hostname, hạn mức đăng ký và hướng dẫn Windows/Linux/Docker.

**Cần nâng Worker tại ddns.20.com.vn lên 0.2.0 trước khi dùng tính năng.**
Chạy schema.sql mới trên D1 hiện có, cập nhật src/index.js rồi Deploy, giữ binding/secrets.
Router/firewall cần cho TCP 443 và UDP WireGuard; DNS/SSL không vượt CGNAT hay tự cấu hình router.
Container cũ cần thêm mapping 443:443 trước khi dùng HTTPS.

Tải bộ cài x64 cho Windows Intel/AMD 64-bit hoặc x86 cho Windows 32-bit. Không cung cấp Windows ARM64. Có gói Linux amd64/arm64 và Docker.

Bản này không ký mã ứng dụng/bộ cài; Windows có thể hiện Unknown publisher. Driver Wintun/WireGuardNT vẫn có chữ ký chính hãng. Đối chiếu SHA-256 trong Assets.

Hướng dẫn: [subdomain/SSL](https://github.com/lehuunghi/vpn/blob/main/docs/subdomain.md) · [cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md).
