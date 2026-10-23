# VPN20 1.4.0

- **Server relay chỉ cần domain và mật khẩu** tự đặt khi cài VPS. Bỏ ô upload ZIP và các bước chép profile/cài frpc riêng.
- Relay VPS **1.1.0** hỏi mật khẩu ẩn ngay sau domain, tự cấp khóa và cổng UDP riêng cho từng máy; giữ kết nối/cổng cũ khi nâng cấp.
- TCP 443 phục vụ cả đăng ký HTTPS và relay. SSL được xác minh trước khi gửi mật khẩu; mật khẩu VPS được băm có salt, giới hạn thử sai. Không lưu mật khẩu trong ứng dụng hoặc trả về API/nhật ký.
- Đăng ký có thể thử lại khi mất phản hồi mà không cấp thêm cổng. Khóa riêng giữ qua khởi động lại, bật/tắt relay và nâng cấp. Đổi mật khẩu đăng ký không ngắt các máy đã được cấp khóa; VPS có lệnh thu hồi riêng từng máy.
- `.conf`, QR và Dashboard dùng Endpoint relay; tắt relay khôi phục điểm kết nối trực tiếp đã lưu. DDNS/SSL quản trị và cổng 51821 giữ nguyên.

**Nâng cấp:** cập nhật bộ relay VPS bằng [lệnh cài đặt mới](https://github.com/lehuunghi/vpn/blob/main/deploy/relay/README.md), đặt mật khẩu nếu đang dùng relay 1.0.0. Nâng VPN20 lên 1.4.0, mở **Cài đặt → Server relay**, nhập domain và mật khẩu để đăng ký máy mới. Máy 1.3.0 đang kết nối tốt tiếp tục dùng cấu hình đã lưu.

Windows x64/x86, Linux amd64/arm64 và Docker `ghcr.io/lehuunghi/vpn20:1.4.0`/`latest`. Không có Windows ARM64. SHA-256 nằm trong Assets. Worker DDNS **0.2.1** không cần triển khai lại.

Ứng dụng/bộ cài Windows chưa ký mã; driver Wintun/WireGuardNT có chữ ký chính hãng.

[Hướng dẫn cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md)
