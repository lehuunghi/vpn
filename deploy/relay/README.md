# VPN20 Relay 1.1.0 — domain và mật khẩu

Cài relay lên VPS, đặt domain và mật khẩu. Trong VPN20 **1.4.0 trở lên**, mở
**Cài đặt → Server relay**, nhập hai thông tin này rồi bấm **Cập nhật relay**.
Không tải ZIP, không chép khóa hay cài frpc riêng trên máy VPN20.

## Chuẩn bị VPS

- Ubuntu 22.04/24.04/26.04 hoặc Debian 12/13, amd64/arm64, có systemd.
- Trỏ bản ghi **A** của domain, ví dụ **relay.20.com.vn**, về IPv4 public VPS.
  Chọn **DNS only**, không bật proxy Cloudflare; không tạo AAAA cho relay này.
- Security group của nhà cung cấp cho phép **TCP 80**, **TCP 443**, **UDP 20001–20100**.
- TCP 80 và 443 chưa được dịch vụ khác dùng. Bộ cài không dừng webserver hiện có.

## Cài đặt bằng một lệnh

Thay domain cuối lệnh bằng domain của bạn:

~~~bash
sudo bash -c 'set -e; f="$(mktemp)"; trap '\''rm -f "$f"'\'' EXIT; curl -fsSL --proto "=https" --proto-redir "=https" https://raw.githubusercontent.com/lehuunghi/vpn/main/deploy/relay/install.sh -o "$f"; bash "$f" --domain "$1"' -- relay.20.com.vn
~~~

Sau domain, bộ cài hỏi **Mật khẩu relay** và **Nhập lại mật khẩu relay**.
Mật khẩu dài **12–256 ký tự**, nhập ẩn; không đặt mật khẩu vào câu lệnh.
Nếu VPS thiếu curl: **sudo apt-get update && sudo apt-get install -y curl**.

Bộ cài kiểm tra SHA-256 của frps và mã quản lý, tự cấp/gia hạn Let's Encrypt,
cấu hình dịch vụ tự khởi động và mở rule trong UFW/firewalld nếu đang hoạt động.
Security group của nhà cung cấp vẫn cần mở như trên. Giữ TCP 80 để gia hạn SSL.
Cấp chứng chỉ mặc định chấp nhận điều khoản ACME, không yêu cầu email.

## Kết nối từ VPN20

1. Nâng ứng dụng VPN20 lên **1.4.0** hoặc mới hơn.
2. Vào **Cài đặt → Server relay**, bật **Dùng server relay**.
3. Nhập **Domain relay** và **Mật khẩu relay** vừa đặt trên VPS.
4. Bấm **Cập nhật relay**, chờ **Đã kết nối relay**.
5. Tải lại `.conf` hoặc quét lại QR trên các thiết bị WireGuard đã cấu hình trước đó.

VPS tự cấp khóa riêng và cổng UDP riêng cho mỗi máy VPN20. Các thiết bị WireGuard
của cùng máy VPN20 dùng chung cổng được cấp. Không cần NAT UDP 51820 trên router.
Đăng ký lại từ cùng dữ liệu VPN20 giữ nguyên khóa/cổng, kể cả khi phản hồi trước
bị mất. Sau khi đăng ký thành công, VPN20 lưu khóa riêng và **không lưu mật khẩu**.
Khởi động lại hoặc bật/tắt relay không cần nhập lại mật khẩu.

Tệp `.conf`, QR và Dashboard dùng đúng Endpoint relay; DDNS không ghi đè nó.
Trang quản trị VPN20 vẫn dùng cổng hiện có, mặc định **51821**. Relay chuyển UDP
WireGuard; public trang quản trị cần đường HTTPS riêng.

## Quản lý trên VPS

~~~bash
sudo vpn20-relay list
sudo vpn20-relay password
sudo vpn20-relay revoke vpn20-TENMAY
sudo vpn20-relay enable vpn20-TENMAY
sudo journalctl -u vpn20-relay.service -n 50 --no-pager
~~~

Lấy tên máy thực tế từ `list`. `password` hỏi ẩn mật khẩu mới và xác nhận lại.
Đổi mật khẩu chặn đăng ký bằng mật khẩu cũ; máy đã đăng ký vẫn dùng khóa riêng.
Muốn ngắt một máy, dùng `revoke`; quyền được kiểm tra lại trong khoảng 10–30 giây.
Cổng của máy bị thu hồi được giữ lại, tránh cấp nhầm Endpoint cũ cho máy khác.

Mật khẩu trên VPS được băm PBKDF2 với salt riêng; có giới hạn đăng ký theo IP
và toàn VPS. SSL luôn được xác minh trước khi gửi mật khẩu. TCP 443 phục vụ cả
đăng ký HTTPS và kết nối relay; các cổng nội bộ 18921/18922 chỉ nghe loopback.

## Nâng cấp relay đang có

Chạy lại lệnh cài đặt trên. Khi nâng từ relay **1.0.0**, bộ cài hỏi mật khẩu để
bật đăng ký mới. Khóa, cổng đã cấp và kết nối VPN20 1.3.0 được giữ nguyên;
không cần đăng ký lại các máy đang kết nối tốt. Chạy lại sau đó giữ mật khẩu.
Nếu kích hoạt dịch vụ mới thất bại, bộ cài khôi phục cấu hình/dịch vụ trước đó.

Giữ cả **/etc/vpn20-relay** và **/var/lib/vpn20-relay** khi sao lưu/di chuyển VPS.
Không xóa dữ liệu VPN20 nếu muốn đăng ký lại mà giữ đúng khóa/cổng đã cấp.

## Cài tự động hoặc dùng chứng chỉ có sẵn

Tải `install.sh`, rồi chạy `sudo bash install.sh --help` để xem tùy chọn.
`--password-file /duong-dan/tep-rieng` đọc mật khẩu từ tệp riêng khi không có
terminal. Không truyền mật khẩu trực tiếp trên dòng lệnh.

Có thể dùng `--cert-file`, `--key-file` để dùng chứng chỉ do hệ thống hiện có
quản lý. Chứng chỉ cần được máy VPN20 tin cậy qua CA hệ thống; không bỏ xác minh
SSL hay tải CA từ một kết nối chưa được tin cậy. Chứng chỉ nguồn được đồng bộ
qua timer và lệnh `sudo vpn20-relay sync-tls`.

Nếu chọn `--control-port` khác 443, nhập `domain:cổng` trong VPN20 và mở cổng đó
trên firewall/security group. Giới hạn mặc định là **1 MB/giây cho mỗi máy VPN20**;
`--limit 0` bỏ giới hạn, `--udp-start`/`--udp-end` đổi dải cổng mà không loại bỏ
các cổng đã cấp. Bộ cài không tự bật/tắt firewall hoặc thay đổi SSH.
