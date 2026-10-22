# VPN20 1.0.0-windows.3

- Thêm relay TCP/UDP và ICMP echo IPv4/IPv6, dùng địa chỉ mạng máy chủ khi ra LAN/Internet, không cần WinNAT hoặc thêm tuyến VPN trên router LAN.
- Giữ adapter VPN Wintun thật để máy chủ chủ động ping/kết nối các thiết bị và cung cấp dịch vụ trên IP VPN.
- Cô lập peer và TCP MSS hoạt động trong relay; khóa/xóa/hết hạn peer đóng các phiên relay.
- Thêm bộ cài và ZIP x64, x86 và ARM64; yêu cầu Windows 10 1607+/11 hoặc Server 2016+.
- Giữ WireGuardNT routed/nat cho nhu cầu định tuyến riêng, giữ UI/API, tài khoản/2FA, thiết bị/QR/cấu hình, thống kê, thời hạn và tự chạy dịch vụ.
- Thêm lựa chọn toàn bộ lưu lượng qua VPN trong giao diện.

Bản thử nghiệm. Xem docs/windows.md để biết phạm vi phiên bản, giao thức và kiểm thử. Relay không chuyển tiếp GRE/ESP/multicast hoặc traceroute ra ngoài. Chưa xác minh trực tiếp Windows 10 Home, Windows 32-bit thật, reboot toàn máy hoặc đăng nhập RDP đồ họa qua router thực tế.
