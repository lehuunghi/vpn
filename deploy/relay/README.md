# VPN20 Relay 1.0.0 — bộ cài riêng cho VPS

Một lệnh cài frps, SSL, gia hạn, systemd, firewall đang hoạt động và profile
client có phân quyền. Không cài VPN20 lên VPS; cổng quản trị VPN20 vẫn là 51821.

VPS: Ubuntu 22.04/24.04/26.04 hoặc Debian 12/13, amd64/arm64, systemd.
Profile có bộ cài client Windows x64 và Linux amd64/arm64; không có Windows ARM64.

## Chuẩn bị

1. Cloudflare: tạo A **relay.20.com.vn** → IPv4 public VPS, **DNS only**.
   Chỉ tạo AAAA nếu IPv6 VPS thật sự hoạt động và nhận được kết nối.
2. Security group của nhà cung cấp cho phép TCP **80**, **443** và UDP
   **20001–20100**. Giữ quyền SSH hiện có.
3. TCP 80 và 443 trên VPS chưa được dịch vụ khác dùng. Bộ cài không dừng
   nginx/Caddy; xem cách dùng chứng chỉ/cổng khác bên dưới.

## Một lệnh

Thay domain cuối lệnh bằng domain của bạn:

~~~bash
sudo bash -c 'set -e; f="$(mktemp)"; trap '\''rm -f "$f"'\'' EXIT; curl -fsSL --proto "=https" --proto-redir "=https" https://raw.githubusercontent.com/lehuunghi/vpn/main/deploy/relay/install.sh -o "$f"; bash "$f" --domain "$1"' -- relay.20.com.vn
~~~

Nếu VPS chưa có curl, cài trước bằng **sudo apt-get update && sudo apt-get install -y curl**.
Bộ cài mặc định đăng ký Let's Encrypt không email và chấp nhận điều khoản ACME
cho thao tác cấp chứng chỉ. Có thể thêm email khi chạy file cục bộ.
Không cần cung cấp API token Cloudflare cho VPS.

Bộ cài tự:

- Tải frps **0.71.1** chính thức theo kiến trúc, kiểm tra SHA-256.
- Tải bộ quản lý theo Git blob bất biến và kiểm tra SHA-256 trước khi chạy.
- Cấp fullchain Let's Encrypt qua HTTP-01; kiểm tra hostname, CA, thời hạn và khóa.
- Chạy dịch vụ **vpn20-relay** bằng tài khoản hệ thống riêng.
- Tự chạy cùng VPS, khởi động lại khi lỗi, đồng bộ TLS qua deploy hook và timer.
- Thêm rule UFW/firewalld nếu chúng đang hoạt động; không tự bật/tắt firewall
  hoặc thay đổi SSH. Security group của nhà cung cấp vẫn cần mở như trên.
- Tạo profile **office** và cấp một cổng UDP trong dải.
- Kiểm tra TLS tại listener; khôi phục cấu hình/dịch vụ cũ nếu kích hoạt thất bại.

## Kết nối máy VPN20

ZIP đầu tiên:

~~~
/etc/vpn20-relay/clients/office/vpn20-relay-office.zip
~~~

Dùng SCP/WinSCP tải ZIP riêng về máy cài VPN20, giải nén và giữ các tệp cùng
thư mục. ZIP có khóa profile; chỉ gửi cho chủ máy này, không công khai ZIP.

Windows x64: mở PowerShell Administrator tại thư mục giải nén:

~~~powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Install-Client.ps1
~~~

Linux:

~~~bash
sudo bash ./install-client.sh
~~~

Bộ cài client tự tải frpc cùng phiên bản, kiểm tra SHA-256, cài cấu hình xác minh
CA/hostname và chạy cùng máy. Windows dùng Scheduled Task SYSTEM; Linux dùng
systemd. Không có bước tắt xác minh TLS.

Trong bản .conf thử nghiệm của điện thoại, đổi:

~~~ini
Endpoint = relay.20.com.vn:20001
~~~

Giữ khóa, DNS và AllowedIPs. Cổng thực tế có trong README của ZIP.
Mỗi máy chủ VPN20 được cấp một cổng; nhiều thiết bị của máy đó cùng dùng cổng này.
Máy VPN20 chủ động kết nối ra VPS, không cần chuyển tiếp UDP 51820 trên router.

**App VPN20 hiện chưa tự quản lý frpc/Endpoint relay.** Cấu hình tải lại từ app
có thể dùng Endpoint DDNS cũ; hãy giữ/sửa Endpoint relay trong cấu hình thiết bị.
Relay này chỉ chuyển UDP WireGuard. Muốn public trang quản trị cần đường TCP/HTTPS
riêng; máy VPN20 vẫn giữ cổng quản trị hiện có, mặc định 51821.

## Thêm, xuất lại và thu hồi profile

~~~bash
sudo vpn20-relay add vanphong2
sudo vpn20-relay add vanphong3 --port 20003 --local-port 51820 --limit 512KB
sudo vpn20-relay list
sudo vpn20-relay export vanphong2
sudo vpn20-relay revoke vanphong2
sudo vpn20-relay enable vanphong2
~~~

Lệnh add in đường dẫn ZIP. Mỗi profile có khóa đăng nhập riêng và chỉ được mở
đúng cổng UDP đã cấp. Cổng bị thu hồi được giữ để không vô tình chuyển lưu lượng
cũ sang người khác. Thu hồi có hiệu lực ở heartbeat kế tiếp, khoảng 10–30 giây.
Một profile dùng cho một máy VPN20.

Giới hạn mặc định **1MB/s cho cả máy VPN20**, khoảng 8 Mbps; **512KB** khoảng
4 Mbps; **0** không giới hạn. Chính sách áp ở VPS; client không tự tăng giới hạn
hoặc đổi cổng/protocol. Plugin chỉ nghe loopback, không có API đăng ký công khai.
Khóa vận chuyển frp trong profile không tự cấp quyền mở cổng: plugin còn kiểm tra
khóa riêng của profile và cấp session ID ngẫu nhiên. Khóa WireGuard ở VPN20/thiết
bị; VPS chuyển tiếp gói WireGuard còn mã hóa.

## Cổng và chứng chỉ tùy chỉnh

~~~bash
curl -fsSL https://raw.githubusercontent.com/lehuunghi/vpn/main/deploy/relay/install.sh -o install-relay.sh
sudo bash install-relay.sh --help
sudo bash install-relay.sh --domain relay.example.com --control-port 7000 --udp-start 21000 --udp-end 21049 --limit 1MB --client office --email admin@example.com
~~~

Nếu VPS đã có nginx/Caddy quản lý SSL, dùng cổng điều phối còn trống và đường dẫn
fullchain/khóa trên VPS:

~~~bash
sudo bash install-relay.sh --domain relay.example.com --control-port 7000 --cert-file /etc/letsencrypt/live/relay.example.com/fullchain.pem --key-file /etc/letsencrypt/live/relay.example.com/privkey.pem
~~~

Giữ đường dẫn nguồn ổn định; bộ đồng bộ kiểm tra mỗi giờ. Có thể gọi ngay sau
khi webserver gia hạn: **sudo vpn20-relay sync-tls**. Chứng chỉ không hợp lệ
không thay thế chứng chỉ đang phục vụ. Với CA nội bộ, cung cấp **--ca-file** rõ ràng.

## Cài lại, đổi VPS và vận hành

Chạy lại giữ domain/cổng/khóa/profile; restart ngắn để áp dụng cấu hình.
Dải mới phải chứa tất cả cổng đã cấp. Đổi domain thì xuất lại ZIP.
Sao lưu riêng toàn bộ **/etc/vpn20-relay** (có khóa, cần quyền bảo vệ), cùng dữ liệu
Let's Encrypt nếu cần giữ tài khoản ACME. Đổi VPS giữ domain, khóa và cổng; cập
nhật DNS rồi kết nối lại client. WireGuard có thể cần tắt/bật tunnel để phân giải
IP mới. Đây không phải chuyển VPS không gián đoạn.

~~~bash
sudo systemctl status vpn20-relay --no-pager
sudo journalctl -u vpn20-relay -n 100 --no-pager
sudo systemctl list-timers vpn20-relay-tls.timer certbot.timer
sudo certbot renew --cert-name relay.20.com.vn --dry-run
~~~

Cổng TCP có listener ngay. Cổng UDP chỉ có listener sau khi frpc đăng ký thành
công. Kiểm tra bằng WireGuard/handshake; cổng frp không phải website HTTP.

## Kiểm thử phát triển

Bash syntax, ShellCheck và **python3 -m unittest discover -s deploy/relay/tests -v**.
Đặt FRP_BIN_DIR tới thư mục frps/frpc 0.71.1 để chạy TLS/UDP thật: nhiều profile,
nhiều client đồng thời, gói 32/148/1452 byte, sai khóa/hostname, chiếm cổng, thu
hồi quyền và backend chính sách lỗi.

CI cài systemd thực tế trên Ubuntu 22.04/24.04, cài lại, kiểm tra giữ identity,
xung đột và rollback; sau đó cài client Windows x64 thực tế. Chứng chỉ fixture
chỉ dùng kiểm thử, không phát hành làm SSL public.
