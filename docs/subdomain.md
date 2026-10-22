# Subdomain và HTTPS tự động

Trong **Cài đặt**, nhập phần tên, ví dụ **vanphong**, rồi bấm **Cập nhật**.
VPN20 dùng **vanphong.vpn.20.com.vn**, tự cập nhật IP Internet và tự cấp/gia hạn
chứng chỉ HTTPS bằng CertMagic, thư viện của Caddy. Mở **https://vanphong.vpn.20.com.vn:8443**.
HTTPS mặc định **TCP 8443**;
không tự đổi sang cổng khác nếu cổng này bị chiếm. HTTP trên LAN giữ cổng 51821.

## Quản trị dịch vụ: nâng Worker hiện có một lần

VPN20 cần Worker **0.2.1** trong [deploy/ddns-worker](../deploy/ddns-worker).
Worker cũ chỉ có đăng ký quản trị/cập nhật IP nên chưa đủ cho nút này.

1. Sao lưu D1 hiện có. Trong D1 Console, chạy toàn bộ
   [schema.sql](../deploy/ddns-worker/schema.sql). CREATE IF NOT EXISTS bổ sung
   bảng mới, giữ nguyên hostname/khóa/dữ liệu cũ; không xóa database.
2. Trong trình sửa mã Worker, thay mã bằng toàn bộ
   [src/index.js](../deploy/ddns-worker/src/index.js), rồi Deploy.
3. Giữ binding **DB**, **CF_ZONE_ID**, Secrets **CF_API_TOKEN** và **ADMIN_TOKEN**
   hiện có. Token Cloudflare cần quyền DNS Write cho zone **20.com.vn**; token
   không được đưa vào VPN20 hay bộ cài.
4. Gắn Custom Domain **ddns.20.com.vn** vào Worker nếu chưa có.
5. GET **https://ddns.20.com.vn/health** phải trả **version: 0.2.1** và features
   chứa **claim**, **dual-stack**, **dns01**, **schema-check**. Health kiểm tra
   binding/cấu hình và schema D1, không thử quyền token Cloudflare hay NAT/HTTPS/VPN.

Worker 0.2.1 tự bổ sung ba bảng phụ còn thiếu trên D1 của bản 0.1.0 khi nhận
yêu cầu đầu tiên. Giữ nguyên bảng hosts và các khóa đã cấp. Với database mới
hoặc thiếu cột trong bảng hiện có, vẫn chạy schema.sql; không xóa database.

Giới hạn đăng ký công khai mặc định: **3 tên/mạng IP/ngày**, **50 tên toàn dịch
vụ**, bao gồm tên cũ. Đặt Variables **CLAIMS_PER_IP_DAY**, **MAX_HOSTS** để điều
chỉnh theo hạn mức Cloudflare/D1 và số bản ghi thực tế của zone.
**SELF_REGISTRATION=false** tạm dừng đăng ký. Thử lại cùng khóa/tên không tốn lượt.

Đây là đăng ký công khai có hạn mức, không phải xác thực khách hàng. Khi mở
dịch vụ rộng rãi, bổ sung cấp quyền/mã kích hoạt hoặc chống bot nếu cần hạn chế
người đăng ký. Người lạ vẫn có thể dùng hết hạn mức toàn cục; không phân phối
ADMIN_TOKEN để giải quyết việc này.

## Windows

1. Cài/nâng cấp VPN20, vào **Cài đặt → Subdomain**, nhập tên, bấm **Cập nhật**.
2. VPN20 tự thêm rule Windows Firewall **VPN20-Public-HTTPS** cho TCP 8443 khi
   bật tính năng. Rule HTTP quản trị LAN vẫn chỉ cho LocalSubnet.
3. Trên router, chuyển tiếp **TCP 8443** về IP LAN máy chủ. Giữ chuyển tiếp
   **UDP 51820** hoặc cổng WireGuard đã chọn.
4. Khi báo **SSL sẵn sàng**, mở **https://ten.vpn.20.com.vn:8443**; thử từ 4G/5G.

Nếu TCP 8443 đã bị chiếm trên máy, VPN20 báo lỗi và giữ đường LAN. Hệ thống
khác đang dùng WAN 443 có thể tiếp tục dùng cổng đó: tạo NAT riêng WAN TCP 8443
về TCP 8443 của VPN20. Ứng dụng không tự sửa router/proxy khác.

## Linux / systemd

Dùng cùng thao tác giao diện. Cho phép TCP 8443 trong firewall máy/VPS.
Chọn đúng công cụ đang quản lý firewall:

- UFW: **sudo ufw allow 8443/tcp**
- firewalld: **sudo firewall-cmd --permanent --add-port=8443/tcp**, rồi
  **sudo firewall-cmd --reload**
- nftables tự quản lý: cho phép TCP 8443 trong chain input hiện có.

Cổng 8443 không yêu cầu quyền bind cổng thấp. DNS-01 không cần cổng vào 80/443.
Quyền root của dịch vụ vẫn cần cho việc quản lý mạng WireGuard/firewall.

## Docker / Podman

Compose bridge đã có **8443:8443/tcp**. Container cũ cần thêm mapping rồi chạy
**docker compose up -d** bằng image/bản build mới. Trình cập nhật trong quản trị
giữ cấu hình container hiện có, không tự thêm mapping thiếu. Dùng
**docker compose up -d --build** nếu xây dựng từ mã nguồn.

Ở chế độ host networking, 8443 trên host phải còn trống. IPv6 public chỉ dùng
khi container thật sự có đường IPv6 vào/ra và firewall cho phép; không bật AAAA
dựa trên IPv6 của host nếu container không nhận được kết nối đó.
Bộ dò IP kết nối trực tiếp từ tiến trình VPN20, không dùng HTTP proxy.
Giữ volume **/data** khi thay container.

## Hành vi và dữ liệu

- Endpoint WireGuard chuyển sang hostname sau khi DNS và chứng chỉ sẵn sàng.
  Thiết bị cũ cần tải/nhập lại .conf nếu Endpoint trước đó là IP/tên khác.
  Đổi IP không đổi tên/chứng chỉ; việc tự phân giải lại DNS của ứng dụng
  WireGuard cần thử trên từng nền tảng.
- Khi đổi tên, tên cũ tiếp tục hoạt động trong lúc cấp tên/SSL mới. Sau thành
  công, A/AAAA do VPN20 quản lý cho tên cũ được gỡ; lỗi gỡ sẽ thử lại.
  Tên cũ vẫn thuộc khóa máy, có thể chọn lại.
- Không ghi đè DNS thủ công của dịch vụ khác; tên xung đột sẽ báo lỗi.
- Mỗi tên có khóa ngẫu nhiên 256 bit, lưu trong database riêng tư. Worker chỉ
  lưu hash. TXT bị giới hạn ở **_acme-challenge.ten.vpn.20.com.vn**; không cho
  yêu cầu tên tùy ý/wildcard.
- IPv4/IPv6 cập nhật trong một lần; chu kỳ **2 phút**, tối thiểu **30 giây**
  tại Worker. Mất IPv6 sẽ xóa AAAA do dịch vụ quản lý. Không dò được cả hai
  họ IP thì giữ DNS đã có và báo lỗi.
- Chứng chỉ/khóa ACME ở **VPN20_DATA_DIR/ddns/certificates**. Sao lưu toàn bộ
  thư mục dữ liệu cùng database; các trình cập nhật giữ thư mục này.
- Cookie HTTPS có Secure; HTTP bằng IP LAN vẫn dùng được nếu không ép
  **VPN20_SECURE_COOKIES=true**. Dùng hostname HTTPS trong LAN cần NAT loopback
  hoặc DNS nội bộ trỏ tên về IP LAN.
- “SSL sẵn sàng” không xác nhận truy cập Internet. DNS-01 cấp chứng chỉ được
  khi chưa mở cổng. CGNAT cần IP public, IPv6 có đường vào hoặc chuyển tiếp.
- SSL bảo vệ trang quản trị, không thay đổi giao thức/mã hóa WireGuard.

## Cấu hình hạ tầng, không thêm trường vào giao diện

| Biến | Mặc định |
| --- | --- |
| VPN20_DDNS_API | https://ddns.20.com.vn |
| VPN20_DDNS_HTTPS | :8443 |
| VPN20_DDNS_ACME_CA | trống: Let's Encrypt production |

Chỉ đặt VPN20_DDNS_ACME_CA khi thử CA staging/nội bộ; trình duyệt không tin
chứng chỉ staging và giao diện ghi rõ chứng chỉ thử nghiệm. Biến có thể đặt
trong JSON Windows, file môi trường systemd hoặc environment Docker.

## Nâng từ bản HTTPS 443

Cập nhật VPN20 lên 1.2.1. Cấu hình mặc định tự chuyển sang 8443 sau khi khởi
động lại; hostname, khóa và chứng chỉ vẫn giữ nguyên. Nếu trước đây đã tự đặt
**VPN20_DDNS_HTTPS=:443**, đổi thành **:8443** hoặc bỏ biến để dùng mặc định.
Windows: **C:\ProgramData\VPN20\config.json** rồi restart dịch vụ VPN20;
Linux: **/etc/vpn20/vpn20.env** rồi **sudo systemctl restart vpn20**;
Docker: sửa environment và mapping **8443:8443/tcp**, tạo lại container.
Đổi NAT/firewall sang TCP 8443. Endpoint UDP WireGuard không thêm cổng 8443.

API trung tâm **https://ddns.20.com.vn** vẫn dùng HTTPS 443. Không đổi API
thành :8443. Cổng 8443 chỉ dành cho trang quản trị trên máy cài VPN20.
Nếu dùng URL Worker khác, đặt **VPN20_DDNS_API=https://ten-worker.tai-khoan.workers.dev**
ở cấu hình hạ tầng rồi restart; URL phải chạy đúng Worker này và dùng HTTPS.

## Chẩn đoán

Mở **https://ddns.20.com.vn/health** trước, phải có **ok: true**. Nếu không
vào được, kiểm tra Custom Domain **ddns.20.com.vn** đã gắn đúng Worker trong
Cloudflare, DNS, SSL và Cloudflare Access/WAF. API cần cho phép tiến trình máy
chủ gọi trực tiếp, không đưa ra trang đăng nhập/challenge tương tác.

Thông báo VPN20 ghi bước API, hostname và HTTP status an toàn, không trả raw
HTML, token hay lỗi SQL. Trường hợp chỉ lỗi trên một máy: kiểm tra DNS, giờ
hệ thống, chứng chỉ gốc và outbound HTTPS của máy đó; đổi 8443 không sửa đường
kết nối tới API trung tâm.

- **not_configured:** kiểm tra binding DB, CF_ZONE_ID và hai Secrets rồi Deploy.
- **schema_outdated:** chạy schema.sql mới trên đúng D1 đang gắn binding DB.
- **database_unavailable:** kiểm tra binding, dịch vụ và hạn mức D1.
- **dns_auth_error:** token cần quyền DNS Write cho đúng zone 20.com.vn.
- **dns_api_unavailable:** Worker không kết nối được Cloudflare; tự thử lại.

- **Cần cập nhật Worker:** Deploy 0.2.1 và chạy schema mới.
- **Tên đã sử dụng:** chọn tên khác; giữ database/khóa cũ để phục hồi tên.
- **Cổng HTTPS:** kiểm tra dịch vụ đang nghe TCP 8443; không tự đổi cổng.
- **Chưa cấp SSL:** kiểm tra Cloudflare Secret/quyền DNS, outbound HTTPS,
  DNS UDP/TCP 53 và log máy chủ. TXT cũ của dịch vụ được dọn khi yêu cầu mới;
  TXT thủ công được giữ nguyên.
- **Có SSL nhưng không vào Internet:** kiểm tra A/AAAA, CGNAT, firewall,
  NAT TCP 8443 và ứng dụng đang nhận cổng WAN.
