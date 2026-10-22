# VPN20 DDNS Worker 0.2.1

API cho **ten.vpn.20.com.vn**. Xem [hướng dẫn](../../docs/subdomain.md).
Không có token Cloudflare thật trong mã nguồn.

## Nâng từ 0.1.0 / 0.2.0

Giữ D1/binding/secrets/domain hiện có. Sao lưu D1, chạy **schema.sql** mới trong
D1 Console, dán toàn bộ **src/index.js** vào Worker rồi Deploy. Không xóa bảng
hosts. Worker tự bổ sung các bảng phụ còn thiếu của bản 0.1.0; database mới
vẫn cần schema.sql. GET /health phải có ok: true, version 0.2.1. Với Wrangler, thay database_id và
CF_ZONE_ID trong wrangler.jsonc rồi chạy:

~~~sh
npx wrangler@4 d1 execute vpn20-ddns --remote --file=schema.sql
npx wrangler@4 deploy
~~~

Secrets qua Dashboard: CF_API_TOKEN (Zone DNS Write, riêng 20.com.vn),
ADMIN_TOKEN (64 ký tự hex ngẫu nhiên). D1 binding tên DB. Custom Domain API:
ddns.20.com.vn. Không đưa secret vào wrangler.jsonc hay máy VPN.

## API

| Endpoint | Quyền | Chức năng |
| --- | --- | --- |
| GET /health | Công khai | Cấu hình/phiên bản và schema D1; không thử quyền CF/DNS/NAT |
| GET /v1/ip | Công khai | IP nguồn CF-Connecting-IP |
| POST /v1/claim | Khóa máy 64 hex, client tự tạo/lưu trước | JSON name, cấp tên; idempotent cùng khóa/tên |
| POST /v1/update | Khóa hostname | JSON ip cũ hoặc ipv4/ipv6; null xóa họ IP |
| POST /v1/acme/present | Khóa hostname | JSON value: 43 ký tự base64url; tạo TXT đúng tên |
| POST /v1/acme/cleanup | Khóa hostname | Xóa TXT sở hữu và khớp value |
| POST /v1/retire | Khóa hostname | Gỡ A/AAAA sở hữu, giữ tên/khóa |
| POST /v1/register | ADMIN_TOKEN | Tương thích cũ; cấp tên/trả khóa một lần |
| POST /v1/admin/hosts/NAME/rotate | ADMIN_TOKEN | Đổi khóa, giữ tên |
| POST /v1/admin/hosts/NAME/revoke | ADMIN_TOKEN | Chặn khóa, không xóa DNS/chứng chỉ đã cấp |

Authorization: Bearer KEY; không đưa khóa vào URL. Body tối đa 4096 byte,
chỉ nhận trường cho phép. Không có CORS; client gọi từ backend VPN20.

A/AAAA DNS only TTL 60, chỉ sửa comment VPN20-DDNS:HOST_ID.
TXT có comment VPN20-ACME:HOST_ID; không sửa/xóa TXT ngoại lai.
Tối đa 4 challenge đồng thời/tên, dọn challenge sở hữu quá 1 giờ,
120 present/tên/giờ; cleanup không tiêu hạn mức present. D1 khóa thao tác;
DDNS tối thiểu 30 giây, HTTP 429 cần thử lại sau.

MAX_HOSTS=50, CLAIMS_PER_IP_DAY=3 là hạn mức khởi đầu, không cam kết sức chứa
Cloudflare. Tên nghỉ vẫn được giữ cho chủ. SELF_REGISTRATION=false tắt claim.
Đăng ký công khai có thể bị dùng hết quota; triển khai rộng cần chống bot/
kiểm soát quyền đăng ký.

Revoke không hủy chứng chỉ đã cấp, không dừng yêu cầu đã chấp nhận trước đó.
Quản trị viên cần xóa DNS/thu hồi chứng chỉ riêng khi cần vô hiệu hóa hoàn toàn.

## Kiểm tra

~~~sh
node --test tests/api.test.js
~~~

Tests dùng SQLite thật cho D1 và giả lập HTTP API Cloudflare. Sau triển khai,
thử thực tế bằng VPN20 và mạng 4G/5G.

## Khi báo lỗi

- **not_configured**: kiểm tra binding DB, CF_ZONE_ID và Secrets CF_API_TOKEN/ADMIN_TOKEN.
- **schema_outdated**: sai/thiếu bảng hoặc cột; chạy schema.sql trên đúng D1 đang gắn DB.
- **database_unavailable**: D1 không truy cập được hoặc hết quota; health không báo sẵn sàng.
- **dns_auth_error / dns_zone_error**: kiểm tra quyền token và Zone ID của 20.com.vn.
- **dns_api_unavailable**: kết nối Worker → Cloudflare lỗi/timeout; thử lại.

API trung tâm vẫn là https://ddns.20.com.vn (443). Trang quản trị VPN20 dùng
https://ten.vpn.20.com.vn:8443, cần NAT/firewall TCP 8443 riêng. DNS-01 không
cần mở cổng vào 80/443. Không đặt Cloudflare Access/trang challenge tương tác
trước API nếu chưa có chính sách cho phép máy VPN gọi trực tiếp.
