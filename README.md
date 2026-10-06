<p align="center">
  <img src="docs/brand/vpn20-mark-256.png" width="128" height="128" alt="Biểu tượng khiên VPN20">
</p>

# VPN20

**VPN20 — Công ty TNHH TN20**

Website: **[20.com.vn](https://20.com.vn)**

Mã nguồn và báo lỗi: **[github.com/lehuunghi/vpn](https://github.com/lehuunghi/vpn)**

VPN20 là máy chủ VPN WireGuard tự lưu trữ, tích hợp giao diện quản trị web trong
một container. **Tiếng Việt là ngôn ngữ mặc định** của giao diện, thông báo thao
tác, định dạng ngày giờ và số liệu.

Khởi động máy chủ, mở giao diện quản trị, tạo thiết bị và quét mã QR. VPN20 chạy
VPN trên mô-đun WireGuard của nhân Linux, quản lý NAT và chuyển tiếp IP, đồng
thời hiển thị thiết bị đang kết nối, tốc độ và lưu lượng sử dụng.

## Chức năng

- **Quản lý thiết bị:** tạo, sửa, bật, ngắt kết nối và xóa. Máy chủ có thể tạo
  cặp khóa cùng khóa chia sẻ trước, cung cấp mã QR và tệp `.conf`; hoặc thiết
  bị tự cung cấp khóa công khai để khóa riêng luôn nằm trên thiết bị. Tự cấp
  địa chỉ VPN hoặc chỉ định thủ công. Hỗ trợ thời hạn kết nối và thay khóa.
- **Theo dõi trực tiếp:** điểm kết nối, lần bắt tay gần nhất, thời gian phiên,
  tốc độ và tổng lưu lượng được cập nhật mỗi 2 giây. Lịch sử theo từng thiết
  bị và toàn hệ thống được ghi mỗi 5 phút, giữ trong 90 ngày.
- **Ngắt và đặt lại phiên:** ngắt kết nối sẽ gỡ thiết bị khỏi giao diện mạng,
  giữ trạng thái ngắt cho đến khi bật lại. Đặt lại phiên cho phép thiết bị
  thiết lập lại kết nối.
- **Hiệu năng:** xử lý trong nhân qua netlink, điều chỉnh sysctl, TCP MSS và
  hỗ trợ mạng host. Tự chuyển sang `wireguard-go` khi thiếu mô-đun nhân.
  Xem [docs/performance.md](docs/performance.md).
- **Bảo mật:** mật khẩu argon2id, xác thực hai bước và mã khôi phục, vai trò
  quản trị/người xem, giới hạn đăng nhập, kiểm tra cùng nguồn, CSP, TLS và
  nhật ký thao tác, kể cả việc xem cấu hình thiết bị. Xem [SECURITY.md](SECURITY.md).
- **Giám sát:** `/api/health` kiểm tra tình trạng; `/metrics` cung cấp dữ liệu
  Prometheus và yêu cầu mã truy cập hoặc phiên đăng nhập.
- **Tự chứa:** một chương trình Go, một cơ sở dữ liệu SQLite trong `/data`,
  không cần dịch vụ bổ sung. Hỗ trợ kiến trúc amd64 và arm64.

## Cài đặt nhanh

```sh
git clone --branch main https://github.com/lehuunghi/vpn.git
cd vpn
# Sửa VPN20_ENDPOINT trong docker-compose.yml thành tên miền hoặc IP công khai
docker compose up -d --build
```

Mở <http://localhost:51821>, tạo quản trị viên đầu tiên và thêm thiết bị.
Dùng ứng dụng WireGuard trên điện thoại để quét mã QR.

Giao diện quản trị mặc định chỉ lắng nghe trên localhost trong tệp Compose.
Để truy cập từ máy khác, bật `VPN20_TLS_SELF_SIGNED: "true"` và ánh xạ địa chỉ
cần thiết, hoặc dùng reverse proxy kết thúc TLS và khai báo proxy trong
`VPN20_TRUSTED_PROXIES`.

`docker-compose.host.yml` chạy trực tiếp trên mạng host để có hiệu năng cao.
Xem [docs/performance.md](docs/performance.md) về các tham số sysctl liên quan.

### Yêu cầu

- Docker hoặc Podman trên Linux, ưu tiên nhân từ 5.6 trở lên. Nhân cũ có thể
  dùng `wireguard-dkms`, hoặc chuyển sang xử lý trong không gian người dùng.
- Container cần quyền `NET_ADMIN` và các tham số chuyển tiếp IP trong Compose.
  Chỉ cần `SYS_MODULE` khi máy chủ chưa nạp và không tự nạp được mô-đun.
- Cổng UDP 51820 hoặc cổng đã chọn phải truy cập được từ Internet.

## Cấu hình

Hạ tầng được cấu hình qua biến môi trường. Các giá trị có thể thay đổi khi
máy chủ đang chạy được lưu trong cơ sở dữ liệu và chỉnh ở mục **Cài đặt**:
địa chỉ kết nối, DNS, định tuyến mặc định, MTU, duy trì kết nối, cô lập thiết
bị, TCP MSS và khóa chia sẻ trước.

| Biến môi trường | Mặc định | Ý nghĩa |
| --- | --- | --- |
| `VPN20_ENDPOINT` | | Tên miền hoặc IP công khai cho thiết bị; cũng được hỏi lúc thiết lập. |
| `VPN20_PORT` | `51820` | Cổng UDP lắng nghe. |
| `VPN20_SUBNET` | `10.8.0.0/24` | Mạng IPv4 của VPN; máy chủ dùng địa chỉ đầu tiên. |
| `VPN20_SUBNET6` | | Mạng IPv6, ví dụ `fd42:42:42::/64`. Để trống để tắt. |
| `VPN20_DNS` | `1.1.1.1, 1.0.0.1` | DNS cấp cho thiết bị khi chạy lần đầu. |
| `VPN20_INTERFACE` | `wg0` | Tên giao diện mạng. |
| `VPN20_EGRESS_INTERFACE` | tự động | Giao diện NAT ra ngoài; tự động dùng tuyến mặc định. |
| `VPN20_HTTP_LISTEN` | `:51821` | Địa chỉ lắng nghe của giao diện quản trị. |
| `VPN20_TLS_SELF_SIGNED` | `false` | Bật HTTPS bằng chứng chỉ tự tạo, lưu trong `/data`. |
| `VPN20_TLS_CERT`, `VPN20_TLS_KEY` | | Bật HTTPS bằng chứng chỉ và khóa của bạn. |
| `VPN20_SECURE_COOKIES` | `false` | Đánh dấu cookie `Secure` khi TLS kết thúc tại proxy. |
| `VPN20_TRUSTED_PROXIES` | | CIDR của proxy được tin cậy khi đọc `X-Forwarded-For`. |
| `VPN20_METRICS_TOKEN` | | Mã bearer cho `/metrics`; phiên đăng nhập cũng được chấp nhận. |
| `VPN20_SESSION_IDLE` | `12h` | Tự đăng xuất sau khoảng thời gian không hoạt động. |
| `VPN20_SESSION_MAX` | `168h` | Thời hạn tối đa của phiên đăng nhập. |
| `VPN20_TRAFFIC_RETENTION` | `2160h` | Thời gian lưu lịch sử lưu lượng, tương đương 90 ngày. |
| `VPN20_POLL_INTERVAL` | `2s` | Chu kỳ đọc bộ đếm giao diện. |
| `VPN20_BACKEND` | `auto` | `kernel`, `userspace` hoặc `mock`; tự động ưu tiên nhân. |
| `VPN20_MANAGE_FIREWALL` | `true` | Đặt `false` nếu máy chủ tự quản lý quy tắc NAT. |
| `VPN20_MANAGE_SYSCTL` | `true` | Đặt `false` nếu máy chủ đã được điều chỉnh sysctl. |
| `VPN20_DATA_DIR` | `/data` | Thư mục cơ sở dữ liệu và tệp TLS. |
| `VPN20_DB` | `/data/vpn20.db` | Đường dẫn SQLite; mặc định theo `VPN20_DATA_DIR`. |
| `VPN20_LOG_LEVEL`, `VPN20_LOG_JSON` | `info`, `false` | Mức log và định dạng JSON. |
| `VPN20_DATA_VOLUME` | `vpn20-data` | Tên volume Docker dùng bởi các tệp Compose. |

## Khôi phục quyền quản trị

```sh
docker exec -it vpn20 vpn20 reset-password admin
```

Lệnh đặt mật khẩu mới, xóa xác thực hai bước và kết thúc các phiên của người
dùng đó. Lệnh dùng cùng cơ sở dữ liệu, không cần khởi động lại.

## Kết nối thiết bị

Dùng ứng dụng WireGuard trên iOS, Android, macOS, Windows, `wg-quick` trên
Linux hoặc bộ định tuyến hỗ trợ WireGuard. Quét mã QR ở thẻ **Cấu hình** của
thiết bị, hoặc tải tệp `.conf`.

Cấu hình mặc định gửi toàn bộ lưu lượng qua VPN. Để định tuyến một phần, đổi
**Định tuyến thiết bị** thành mạng VPN hoặc mạng cần truy cập.

## API

Giao diện web sử dụng `/api/…` cùng cookie phiên đăng nhập. Một số endpoint:
`GET /api/peers`, `POST /api/peers`, `GET /api/peers/{id}/config`,
`POST /api/peers/{id}/disable`. Cấu trúc dữ liệu nằm trong
`internal/server/api.go`.

Yêu cầu từ trình duyệt khác nguồn bị từ chối khi không có header
`Sec-Fetch-Site` hoặc `Origin` hợp lệ. Hãy gọi từ cùng nguồn hoặc từ chương
trình phía máy chủ. Tên trường, endpoint, vai trò `admin`/`viewer` và mã sự
kiện API giữ nguyên; giao diện hiển thị nhãn bằng tiếng Việt.

## Biên dịch từ mã nguồn

```sh
cd web && npm ci && npm run build && cd ..
go build ./cmd/vpn20
```

Giao diện được nhúng vào chương trình. `docker build -t vpn20 .` thực hiện cả
hai bước. Xem [CONTRIBUTING.md](CONTRIBUTING.md) để phát triển với chế độ mô
phỏng không cần quyền quản trị hệ thống.

## Nâng cấp bản cài đặt đang dùng

Sao lưu volume dữ liệu trước khi nâng cấp. Bản VPN20 dùng biến môi trường
`VPN20_*`, lệnh `vpn20`, cơ sở dữ liệu mặc định `/data/vpn20.db` và bảng
tường lửa `vpn20`.

Đặt `VPN20_DB` trỏ đến cơ sở dữ liệu hiện có và `VPN20_DATA_VOLUME` trùng tên
volume đang dùng để giữ người dùng, khóa, thiết bị và lịch sử. Dừng container
cũ trước khi khởi động VPN20; gỡ bảng tường lửa cũ sau khi dừng.

Phiên đăng nhập trình duyệt cũ sẽ kết thúc; hãy đăng nhập lại. Mã xác thực
đang dùng vẫn hoạt động; mã QR đăng ký mới mang tên VPN20.

## Giấy phép

AGPL-3.0-or-later. Xem [LICENSE](LICENSE) và [NOTICE](NOTICE) để biết thông
tin bản quyền nguồn gốc và giấy phép bên thứ ba. Thương hiệu VPN20 và các
chỉnh sửa được duy trì bởi **[Công ty TNHH TN20](https://20.com.vn)**.
