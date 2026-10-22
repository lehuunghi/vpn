# VPN20 1.1.2

- Bộ cài Windows tự bật LAN khi cài mới, cài im lặng và nâng cấp từ bản chỉ dùng localhost. Cập nhật từ quản trị cũng tự áp dụng bước này.
- Giữ cổng và các thiết lập khác, kiểm tra dịch vụ sau cài, khôi phục cấu hình khi lỗi. Windows Firewall mở cổng quản trị TCP với phạm vi LocalSubnet.
- Gỡ hoàn toàn chức năng cấp tên miền/subdomain tự động, API, tác vụ nền, cấu hình, các file Docker liên quan và mục “Truy cập LAN và tên miền” trong Cài đặt.
- Bỏ đoạn hướng dẫn chuyển tiếp TCP/UDP/ping và chọn tuyến LAN khỏi Bảng điều khiển.
- Giữ AllowedIPs dạng ô nhập đơn giản, quản lý thiết bị, TCP/UDP/ping, IPv4/IPv6, TCP MSS, thống kê và nút cập nhật phiên bản.

Tải bộ cài x64 cho Windows Intel/AMD 64-bit hoặc x86 cho Windows 32-bit. Không cung cấp Windows ARM64. Có gói Linux amd64/arm64 và Docker.

Bản này không ký mã ứng dụng/bộ cài; Windows có thể hiện Unknown publisher. Driver Wintun/WireGuardNT vẫn có chữ ký chính hãng. Đối chiếu SHA-256 trong Assets.

Hướng dẫn: [cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md) · [truy cập LAN](https://github.com/lehuunghi/vpn/blob/main/docs/lan-access.md).
