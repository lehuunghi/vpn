# Subdomain và HTTPS tự động

Trong **Cài đặt**, nhập phần tên, ví dụ **vanphong**, rồi bấm **Cập nhật**.
VPN20 dùng **vanphong.vpn.20.com.vn**, tự cập nhật IP Internet và tự cấp/gia hạn
chứng chỉ HTTPS bằng CertMagic, thư viện của Caddy. HTTPS mặc định **TCP 443**;
không tự đổi sang cổng khác nếu cổng này bị chiếm. HTTP trên LAN giữ cổng 51821.

## Quản trị dịch vụ: nâng Worker hiện có một lần

VPN20 cần Worker **0.2.0** trong [deploy/ddns-worker](../deploy/ddns-worker).
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
5. GET **https://ddns.20.com.vn/health** phải trả **version: 0.2.0** và features
   chứa **claim**, **dual-stack**, **dns01**. Health không chứng minh NAT/HTTPS/VPN
   tại các máy đã hoạt động.

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
2. VPN20 tự thêm rule Windows Firewall **VPN20-Public-HTTPS** cho TCP 443 khi
   bật tính năng. Rule HTTP quản trị LAN vẫn chỉ cho LocalSubnet.
3. Trên router, chuyển tiếp **TCP 443** về IP LAN máy chủ. Giữ chuyển tiếp
   **UDP 51820** hoặc cổng WireGuard đã chọn.
4. Khi báo **SSL sẵn sàng**, mở **https://ten.vpn.20.com.vn**; thử từ 4G/5G.

Nếu TCP 443 đã bị chiếm trên máy, VPN20 báo lỗi và giữ đường LAN. Nếu hệ thống
khác trong LAN đang nhận WAN 443, cần chuyển NAT về VPN20 hoặc dùng reverse
proxy chung định tuyến theo hostname. Ứng dụng không tự sửa router/proxy khác.

## Linux / systemd

Dùng cùng thao tác giao diện. Cho phép TCP 443 trong firewall máy/VPS.
Chọn đúng công cụ đang quản lý firewall:

- UFW: **sudo ufw allow 443/tcp**
- firewalld: **sudo firewall-cmd --permanent --add-service=https**, rồi
  **sudo firewall-cmd --reload**
- nftables tự quản lý: cho phép TCP 443 trong chain input hiện có.

Dịch vụ native chạy root có thể bind 443. Nếu tự chạy bằng tài khoản khác,
cấp quyền bind cổng thấp phù hợp. Không cần cổng 80 cho DNS-01.

## Docker / Podman

Compose bridge đã có **443:443/tcp**. Container cũ cần thêm mapping rồi chạy
**docker compose up -d** bằng image/bản build mới. Trình cập nhật trong quản trị
giữ cấu hình container hiện có, không tự thêm mapping thiếu. Dùng
**docker compose up -d --build** nếu xây dựng từ mã nguồn.

Ở chế độ host networking, 443 trên host phải còn trống. IPv6 public chỉ dùng
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
| VPN20_DDNS_HTTPS | :443 |
| VPN20_DDNS_ACME_CA | trống: Let's Encrypt production |

Chỉ đặt VPN20_DDNS_ACME_CA khi thử CA staging/nội bộ; trình duyệt không tin
chứng chỉ staging và giao diện ghi rõ chứng chỉ thử nghiệm. Biến có thể đặt
trong JSON Windows, file môi trường systemd hoặc environment Docker.

## Chẩn đoán

- **Cần cập nhật Worker:** Deploy 0.2.0 và chạy schema mới.
- **Tên đã sử dụng:** chọn tên khác; giữ database/khóa cũ để phục hồi tên.
- **Cổng HTTPS:** kiểm tra dịch vụ đang nghe TCP 443; không fallback 8443.
- **Chưa cấp SSL:** kiểm tra Cloudflare Secret/quyền DNS, outbound HTTPS,
  DNS UDP/TCP 53 và log máy chủ. TXT cũ của dịch vụ được dọn khi yêu cầu mới;
  TXT thủ công được giữ nguyên.
- **Có SSL nhưng không vào Internet:** kiểm tra A/AAAA, CGNAT, firewall,
  NAT TCP 443 và ứng dụng đang nhận cổng WAN.
