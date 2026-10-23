# VPN20 1.3.0

- Thêm **Cài đặt → Server relay**: bật/tắt relay, nhập domain VPS và ZIP profile do **vpn20-relay add/export** cấp.
- Client relay tích hợp trong ứng dụng: dùng được với bộ cài Windows x64/x86, Linux amd64/arm64 và Docker. Không cần cài/chạy frpc riêng.
- Tự đọc cổng/khóa từ profile, xác minh CA/hostname SSL và nối tới cổng WireGuard thực tế trên máy. Tự kết nối lại và khôi phục profile khi VPN20 khởi động lại.
- Tệp .conf và mã QR tự dùng domain cùng cổng UDP relay. Khi tắt relay, cấu hình xuất lại dùng điểm kết nối trực tiếp đã lưu.
- DDNS/SSL quản trị hoạt động riêng, không ghi đè Endpoint relay. Trang quản trị giữ cổng mặc định **51821**.
- Profile chỉ được quản trị viên nhập; không thực thi script/plugin/includes từ ZIP. Khóa giữ trong cơ sở dữ liệu riêng, không có trong API trạng thái hoặc nhật ký.
- Docker có thêm tag phiên bản **1.3.0** cùng **latest**.

**Sử dụng:** nâng cấp VPN20, vào Cài đặt → Server relay, bật Dùng server relay,
nhập domain và chọn ZIP profile. Chờ Đã kết nối relay rồi tải lại .conf/quét lại
QR trên thiết bị. Domain đơn lẻ không thay thế profile có khóa và cổng riêng.

VPS vẫn dùng [bộ cài relay riêng](https://github.com/lehuunghi/vpn/blob/main/deploy/relay/README.md).
Nếu trước đây đã cài client frpc riêng bằng cùng profile, dừng client đó trước
khi bật relay trong VPN20 để tránh tranh chấp kết nối.

Đã có kiểm thử TLS/UDP thực tế, sai chứng chỉ/sai khóa, mất mạng/thu hồi quyền,
tự kết nối lại, giữ profile sau khởi động lại và DDNS không đổi Endpoint relay.
Các gói phát hành được kiểm tra cài đặt/nâng cấp trên Linux và Windows.
Kiểm thử SSL dùng CA cục bộ; kết nối thực tế vẫn bắt buộc chứng chỉ hợp lệ.

Bộ cài Windows x64/x86, Linux amd64/arm64 và Docker; không có Windows ARM64.
Ứng dụng/bộ cài chưa ký mã; driver Wintun/WireGuardNT có chữ ký chính hãng.
SHA-256 nằm trong Assets. Worker DDNS **0.2.1** giữ nguyên, không cần triển khai lại.

[Hướng dẫn cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md)
