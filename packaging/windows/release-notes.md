# VPN20 1.1.0

- Thêm relay TCP/UDP và ICMP echo IPv4/IPv6, dùng địa chỉ mạng máy chủ khi ra LAN/Internet, không cần WinNAT hoặc thêm tuyến VPN trên router LAN.
- Giữ adapter VPN Wintun thật để máy chủ chủ động ping/kết nối các thiết bị và cung cấp dịch vụ trên IP VPN.
- Cô lập peer và TCP MSS hoạt động trong relay; khóa/xóa/hết hạn peer đóng các phiên relay.
- Thêm bộ cài và ZIP x64 và x86; yêu cầu Windows 10 1607+/11 hoặc Server 2016+.
- Giữ WireGuardNT routed/nat cho nhu cầu định tuyến riêng, giữ UI/API, tài khoản/2FA, thiết bị/QR/cấu hình, thống kê, thời hạn và tự chạy dịch vụ.
- Thêm lựa chọn toàn bộ lưu lượng qua VPN trong giao diện.

- Thêm nút cập nhật đầy đủ trong quản trị, kiểm tra SHA-256 từ GitHub, sao lưu và phục hồi khi lỗi. Windows nhận bộ cài chưa ký nếu quản trị viên không ghim chứng chỉ.
- Thêm gói Linux/systemd, cập nhật Docker qua cấu hình socket riêng và hướng dẫn cài đặt/HTTPS chi tiết.
- Chữ ký ứng dụng/bộ cài là tùy chọn; bản này phát hành không ký mã. Driver Wintun/WireGuardNT vẫn có chữ ký chính hãng.
- Kèm gói Linux amd64/arm64 cho systemd và hướng dẫn Docker/Compose, HTTPS, sao lưu và cập nhật.

Tải `windows-x64-setup.exe` cho Windows Intel/AMD 64-bit hoặc `windows-x86-setup.exe` cho Windows 32-bit; các gói ZIP dành cho quản trị viên triển khai thủ công. Đối chiếu `SHA256SUMS.txt` trước khi chạy. Windows có thể hiển thị Unknown publisher vì ứng dụng/bộ cài chưa ký; đây không phải chứng chỉ nhà phát hành được tin cậy.

Xem [hướng dẫn cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md). Relay không chuyển tiếp GRE/ESP/multicast hoặc traceroute ra ngoài. Chưa xác minh trực tiếp Windows 10 Home, Windows 32-bit thật, reboot toàn máy hoặc đăng nhập RDP đồ họa qua router thực tế.
