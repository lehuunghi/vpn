# Subdomain và HTTPS trên cổng quản trị

Trong **Cài đặt**, nhập phần tên, ví dụ **vanphong**, rồi bấm **Cập nhật**.
VPN20 dùng **vanphong.vpn.20.com.vn**, tự cập nhật IP Internet và tự cấp/gia hạn
chứng chỉ HTTPS bằng CertMagic, thư viện của Caddy. Địa chỉ quản trị mặc định:

**https://vanphong.vpn.20.com.vn:51821**

HTTPS dùng chính TCP 51821 của trang quản trị. Không mở listener/cổng HTTPS phụ.
Nếu đã đổi VPN20_HTTP_LISTEN, HTTPS dùng cổng quản trị đã chọn. API, giao diện,
SSE và cookie HTTPS đều chạy trên cổng này.

## Nâng cấp lên VPN20 1.2.2

1. Cài/nâng VPN20 lên 1.2.2, giữ nguyên database và thư mục dữ liệu.
2. Mở/chuyển tiếp **TCP 51821** về máy chủ; giữ UDP WireGuard hiện có.
3. Docker: dùng Compose mới, giữ mapping quản trị **51821:51821/tcp**, bỏ
   mapping HTTPS phụ của bản cũ rồi **docker compose up -d** để tạo lại container.
4. Xóa biến **VPN20_DDNS_HTTPS** cũ khỏi environment/JSON nếu từng tự đặt;
   biến này đã bị loại bỏ. Chỉ VPN20_HTTP_LISTEN quyết định cổng quản trị.
5. Vào Cài đặt, giữ tên subdomain và bấm Cập nhật. Khi SSL sẵn sàng, mở link HTTPS.

Bộ cài Windows gỡ khóa cấu hình cổng phụ khi nâng cấp và dịch vụ dọn rule HTTPS
cũ lúc khởi động. Rule mới chỉ mở cổng quản trị sau khi chứng chỉ đã sẵn sàng.
Hostname, khóa máy, tài khoản ACME và chứng chỉ được giữ khi cập nhật.
Nếu Worker đang chạy **0.2.1**, không cần Deploy lại để dùng bản sửa SSL này.

HTTP bằng IP LAN/loopback vẫn dùng để thiết lập, xem trạng thái và khôi phục,
kể cả khi đang cấp SSL. Sau khi có hostname hoạt động, HTTP gọi bằng hostname
hoặc từ Internet chuyển hướng sang HTTPS trên cùng cổng. Cổng TCP chỉ bind
một lần; không ngắt trang LAN khi chứng chỉ được cấp/gia hạn.

## Quản trị dịch vụ DDNS

Worker **0.2.1** ở [deploy/ddns-worker](../deploy/ddns-worker) hỗ trợ đăng ký
hostname, IPv4/IPv6 và TXT DNS-01. Khi nâng từ Worker cũ:

1. Sao lưu D1. Trong D1 Console, chạy toàn bộ
   [schema.sql](../deploy/ddns-worker/schema.sql) nếu database mới/thiếu schema.
   CREATE IF NOT EXISTS giữ dữ liệu và khóa cũ; không xóa database.
2. Thay mã Worker bằng [src/index.js](../deploy/ddns-worker/src/index.js), Deploy.
3. Giữ binding **DB**, Variable **CF_ZONE_ID**, Secrets **CF_API_TOKEN** và
   **ADMIN_TOKEN**. Token Cloudflare cần DNS Write cho zone **20.com.vn**.
4. Gắn Custom Domain **ddns.20.com.vn** vào đúng Worker.
5. Mở **https://ddns.20.com.vn/health**: phải có **ok: true**, **version: 0.2.1**,
   features claim, dual-stack, dns01, schema-check. Health kiểm tra cấu hình/schema,
   chưa thử quyền token Cloudflare hay đường vào máy VPN.

Worker tự bổ sung các bảng phụ còn thiếu của bản 0.1.0. Các cột/bảng hosts
hiện có không bị sửa/xóa. Mỗi máy tự tạo khóa 256 bit riêng; Worker chỉ lưu hash.
CF_API_TOKEN và ADMIN_TOKEN chỉ nằm ở Worker, không được đưa vào máy VPN/bộ cài.

Mặc định **3 tên/mạng IP/ngày**, **50 tên toàn dịch vụ**. Variables
CLAIMS_PER_IP_DAY/MAX_HOSTS điều chỉnh hạn mức; SELF_REGISTRATION=false tắt đăng ký.
Tên cũ vẫn giữ cho chủ; thử lại cùng khóa/tên không tiêu lượt. Đây là đăng ký
công khai có hạn mức; triển khai rộng cần kiểm soát quyền đăng ký/chống bot.

## Windows

1. Cài/nâng VPN20 x64/x86; vào Cài đặt → Subdomain → Cập nhật.
2. Chứng chỉ sẵn sàng thì VPN20 tự thêm rule Windows Firewall **VPN20-Public-HTTPS**
   cho chính cổng quản trị, mặc định TCP 51821. Rule LAN vẫn giới hạn LocalSubnet.
3. Router chuyển tiếp TCP 51821 về IP LAN cố định của máy chủ và UDP WireGuard.
4. Dùng **https://ten.vpn.20.com.vn:51821**, thử từ mạng 4G/5G.

Cấu hình máy: **C:\ProgramData\VPN20\config.json**. Sau khi sửa, restart dịch vụ
VPN20. Log: **C:\ProgramData\VPN20\vpn20.log**. Không thêm cổng HTTPS riêng.

## Linux / systemd

Dùng cùng thao tác giao diện. Cho phép TCP 51821 trong firewall đang quản lý:

- UFW: **sudo ufw allow 51821/tcp**.
- firewalld: **sudo firewall-cmd --permanent --add-port=51821/tcp**, rồi reload.
- nftables: cho phép TCP 51821 trong chain input hiện có.

Môi trường: **/etc/vpn20/vpn20.env**, restart bằng **sudo systemctl restart vpn20**.
Xem log: **sudo journalctl -u vpn20 -n 100 --no-pager**. Quyền root của dịch vụ
vẫn cần để quản lý WireGuard/firewall; cổng 51821 không cần quyền bind cổng thấp.

## Docker / Podman

Compose bridge chỉ cần TCP 51821 cho quản trị và UDP WireGuard, không có mapping
HTTPS phụ. Cập nhật image rồi **docker compose up -d**; nếu build mã nguồn dùng
**docker compose up -d --build**. Giữ volume /data. Trình cập nhật quản trị giữ
port mapping hiện có; muốn bỏ mapping cũ cần sửa Compose và tạo lại container.

Host networking dùng cổng quản trị trên host. IPv6 public chỉ bật khi container
thật sự có đường IPv6 vào/ra. Dò IP kết nối trực tiếp từ tiến trình VPN20, không
qua HTTP proxy. Xem log bằng **docker compose logs --tail=100 vpn20**.

## Cấp SSL và xử lý lỗi

A/AAAA đúng IP chỉ hoàn tất bước DDNS. SSL còn cần TXT ở
**_acme-challenge.ten.vpn.20.com.vn** được CA xác nhận.

VPN20 tạo/xóa TXT qua Worker; kiểm tra TXT công khai qua DNS-over-HTTPS của
Cloudflare/Google. Không dò zone SOA/NS hoặc buộc máy VPN truy vấn trực tiếp
1.1.1.1/8.8.8.8 trên cổng 53. DNS hệ thống vẫn cần hoạt động để kết nối các API.
Cấp/gia hạn chạy không tương tác, không hỏi email trên stdin của dịch vụ.
CA vẫn tự xác minh domain; không bỏ kiểm tra TXT hay xác minh chứng chỉ TLS.

Cần outbound HTTPS tới Worker, máy chủ ACME và ít nhất một trong
cloudflare-dns.com/dns-query, dns.google/dns-query. DNS-01 không cần cổng vào 80/443.
API trung tâm **https://ddns.20.com.vn** vẫn dùng HTTPS 443; không thêm :51821 vào API.

Giao diện ghi giai đoạn đang tạo TXT/chờ DNS/chờ CA và giữ lỗi khi chờ thử lại.

| Mã lỗi | Kiểm tra |
| --- | --- |
| dns_txt_not_visible | TXT đúng giá trị đã hiện trên DNS công khai; delegation/NS đúng |
| dns_doh_unavailable | Outbound HTTPS, proxy/firewall tới Cloudflare DNS/Google DNS |
| dns_servfail | Nameserver và DNSSEC của 20.com.vn |
| acme_caa | CAA có cho letsencrypt.org cấp chứng chỉ không |
| acme_rateLimited | Hạn mức CA; chờ, tránh bấm cập nhật liên tục |
| ca_tls / ca_network / ca_dns | Chứng chỉ gốc, giờ máy, kết nối HTTPS/DNS tới CA |
| certificate_storage | Quyền đọc/ghi thư mục dữ liệu/chứng chỉ |
| schema_outdated / database_unavailable | Schema/binding/hạn mức D1 |
| dns_auth_error | CF_API_TOKEN, quyền DNS Write và CF_ZONE_ID |

Lỗi chi tiết nằm ở dòng **automatic HTTPS certificate failed** trong log.
Không gửi private key, token hoặc toàn bộ database khi chẩn đoán. Tự thử lại
SSL theo 5/10/20/40 phút, tối đa 1 giờ; nút Cập nhật thử lại ngay khi đã sửa hạ tầng.
DDNS vẫn cập nhật IP trong khi chờ SSL. Chứng chỉ cũ/cached vẫn phục vụ HTTPS
khi API DDNS tạm lỗi; gia hạn chạy trong nền.

## Dữ liệu và giới hạn mạng

- Chứng chỉ/khóa ACME ở **VPN20_DATA_DIR/ddns/certificates**; sao lưu toàn bộ
  thư mục dữ liệu cùng database. Cookie HTTPS có Secure; SSL chỉ bảo vệ quản trị.
- Endpoint WireGuard chuyển hostname sau khi DNS/chứng chỉ sẵn sàng. Thiết bị cũ
  tải/nhập lại .conf nếu Endpoint đổi; cổng UDP không đổi thành cổng quản trị.
- IPv4/IPv6 cập nhật cùng lần, chu kỳ 2 phút; mất IPv6 xóa AAAA do dịch vụ quản lý.
  Không dò được cả hai thì giữ DNS hiện có. DNS thủ công không bị ghi đè.
- Đổi tên giữ tên cũ hoạt động trong lúc cấp tên mới; sau thành công gỡ A/AAAA
  do dịch vụ quản lý ở tên cũ, giữ quyền sở hữu tên và khóa.
- SSL sẵn sàng không khẳng định NAT/Internet đã thông. Cần IP public/IPv6 có
  đường vào; DNS/SSL không vượt CGNAT hoặc tự sửa router. LAN dùng hostname
  cần DNS nội bộ trỏ tên về IP LAN hoặc NAT loopback.

## Cấu hình hạ tầng

| Biến | Mặc định |
| --- | --- |
| VPN20_HTTP_LISTEN | :51821; bộ cài Windows/Linux bật 0.0.0.0:51821 cho LAN |
| VPN20_DDNS_API | https://ddns.20.com.vn |
| VPN20_DDNS_ACME_CA | trống: Let's Encrypt production |

ACME staging/nội bộ chỉ dành thử nghiệm; trình duyệt không tin chứng chỉ staging.
Giao diện đánh dấu chứng chỉ thử nghiệm. Nếu dùng URL Worker khác, đặt
VPN20_DDNS_API=https://ten-worker.tai-khoan.workers.dev rồi restart dịch vụ.
