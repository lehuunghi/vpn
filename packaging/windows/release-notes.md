# VPN20 1.1.1

- Đơn giản hóa AllowedIPs: bỏ hai nút chọn tuyến và các đoạn mô tả cũ trong Cài đặt.
- Cài mới Windows/Linux và Compose bridge cho phép truy cập quản trị bằng IP LAN. Windows tự mở đúng cổng TCP với phạm vi LocalSubnet.
- Thêm `vpn20.exe enable-lan` cho bản Windows đã cài: giữ cổng và dữ liệu, khởi động lại dịch vụ, khôi phục cấu hình nếu lỗi. Nâng cấp giữ listener đã cấu hình.
- Thêm chọn subdomain Cloudflare trong quản trị, tạo bản ghi A/DNS-only, nhận IPv4 công khai tự động hoặc nhập tay, đồng bộ DDNS mỗi 5 phút và dùng tên làm endpoint VPN nếu được chọn.
- API token nằm trên máy chủ/file secret, không trả về trình duyệt. Không ghi đè bản ghi DNS có sẵn; chỉ sửa/xóa bản ghi VPN20 của máy đó.
- Thêm hướng dẫn LAN, firewall, Cloudflare DNS, HTTPS/Caddy và Tunnel cho CGNAT; kèm Compose override cho DNS và Tunnel. Tunnel public hostname chỉ dùng cho web quản trị, không thay endpoint UDP WireGuard.
- Giữ relay TCP/UDP/ping IPv4/IPv6, TCP MSS, tài khoản/2FA, thiết bị/QR/cấu hình, thống kê và nút cập nhật toàn bộ từ 1.1.0.

Tải bộ cài x64 cho Windows Intel/AMD 64-bit hoặc x86 cho Windows 32-bit. Không cung cấp Windows ARM64. Gói Linux amd64/arm64 và Docker tiếp tục được hỗ trợ.

Bản này không ký mã ứng dụng/bộ cài; Windows có thể hiện Unknown publisher. Driver Wintun/WireGuardNT vẫn có chữ ký chính hãng. Đối chiếu SHA-256 trong Assets; không có chứng chỉ nhà phát hành công cộng giả lập.

Hướng dẫn: [cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md) · [LAN và domain](https://github.com/lehuunghi/vpn/blob/main/docs/public-access.md). Cấu hình domain/Zone ID/token và router/HTTPS thực tế do chủ máy thực hiện; bản ghi DNS thành công không xác nhận cổng Internet đã mở.
