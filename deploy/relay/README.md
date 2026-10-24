# VPN20 Relay 1.1.1 — domain và mật khẩu

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

Chỉ coi là cài xong khi thấy **Đã cài VPN20 relay 1.1.1**. Kiểm tra trên VPS:

~~~bash
sudo systemctl is-active vpn20-relay.service
sudo vpn20-relay list
~~~

Dịch vụ cần trả về `active`. Danh sách máy có thể trống trước khi kết nối VPN20.
Không cần chạy `vpn20-relay password` ngay sau cài: mật khẩu đã đặt trong bộ cài.

Nếu bản 1.1.0 dừng ở **Cần terminal để đặt mật khẩu** dù đang mở SSH, tải lại
bộ cài hiện hành và chạy lại lệnh trên; bản 1.1.1 sửa bước đọc mật khẩu terminal.
Dòng Domain/TCP/UDP chỉ là xác nhận tham số, chưa báo cài thành công. Lệnh
`vpn20-relay password` chỉ có sau khi bộ cài tạo chương trình quản lý.
Nếu gặp `sudo: vpn20-relay: command not found` sau lỗi này, chạy lại bộ cài ở trên.
Nếu đã báo cài thành công nhưng vẫn không tìm thấy lệnh, thử
`sudo /usr/local/bin/vpn20-relay list`; lệnh này dùng đường dẫn đầy đủ.

Với SSH chạy lệnh từ xa, dùng `ssh -t` để cấp terminal. Với công cụ chạy lệnh
tự động không có TTY, `--password-file` là đường dẫn tới **tệp trên VPS chứa
mật khẩu trong một dòng**, không phải mật khẩu truyền trực tiếp và không phải
ZIP profile. Dùng tệp chỉ root được đọc và xóa sau khi cài.

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

## Đổi giới hạn tốc độ relay

Mặc định mỗi máy VPN20 được giới hạn **1 MB/giây, xấp xỉ 8 Mbps**. Các thiết bị
WireGuard kết nối vào cùng máy VPN20 chia sẻ mức này. Giá trị `0` bỏ giới hạn
của relay; tốc độ thực tế vẫn phụ thuộc đường truyền, VPS và máy VPN20.

| Giá trị | Ý nghĩa |
| --- | --- |
| `0` | Không giới hạn tốc độ |
| `1MB` | Khoảng 8 Mbps cho mỗi máy VPN20 |
| `10MB` | Khoảng 80 Mbps cho mỗi máy VPN20 |
| `100MB` | Khoảng 800 Mbps cho mỗi máy VPN20 |
| `512KB` | Khoảng 4 Mbps cho mỗi máy VPN20 |

Chạy đoạn sau **trên VPS relay đã cài xong** để đổi giới hạn cho **tất cả máy
đã đăng ký và mức mặc định cho máy đăng ký mới**. Giữ `"0"` để bỏ giới hạn;
thay giá trị trong dòng `limit = ...` bằng `"10MB"`, `"100MB"` hoặc `"1MB"`
nếu muốn đặt lại giới hạn. Đoạn lệnh giữ nguyên mật khẩu, khóa, cổng và trạng
thái cho phép/thu hồi của từng máy.

~~~bash
sudo python3 - <<'PY'
import contextlib, grp, runpy

relay = runpy.run_path("/usr/local/lib/vpn20-relay/relay.py")
limit = relay["bandwidth"]("0")
m = relay["Manager"](gid=grp.getgrnam("vpn20-relay").gr_gid)

with m.lock():
    s = m.state()
    s["default_limit"] = limit
    for client in s["clients"].values():
        client["limit"] = limit
    with contextlib.closing(m.database()) as db, db:
        db.execute("UPDATE clients SET bandwidth = ?", (limit,))
    m.save(s)

print("Đã đặt giới hạn tốc độ relay:", limit)
PY
~~~

Sau khi thấy thông báo thành công, khởi động lại relay để các kết nối hiện có
nhận mức mới, rồi kiểm tra:

~~~bash
sudo systemctl restart vpn20-relay.service
sudo systemctl is-active vpn20-relay.service
sudo vpn20-relay list
~~~

Dịch vụ cần trả về `active`; cột ngay trước `enabled` hoặc `revoked` hiển thị
giới hạn mới, ví dụ `0` thay cho `1MB`. Kết nối ngắt vài giây rồi VPN20 tự nối
lại. Với client tích hợp trong VPN20 1.4.0 trở lên, không cần nhập lại mật khẩu
hay tải lại `.conf`/QR trên thiết bị WireGuard.

Khi **cài mới**, có thể thêm `--limit 0` vào lệnh chạy bộ cài để đặt mặc định
không giới hạn ngay từ đầu. Chạy lại bộ cài với `--limit` chỉ đổi mức mặc định;
máy đã đăng ký vẫn giữ mức riêng, nên dùng đoạn lệnh trên để đổi cả máy đang dùng.
Máy còn dùng frpc riêng từ profile cũ có thể có thêm giới hạn trong `frpc.toml`;
cần đồng bộ giới hạn phía client đó nếu muốn tăng tốc độ.

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

Ví dụ: chuẩn bị tệp một dòng trên VPS, giới hạn quyền đọc rồi chạy bộ cài.
Các bước tạo/sửa tệp sau dành cho người chuẩn bị lần đầu; hệ thống tự động có
thể cấp tệp từ kho bí mật của bạn thay cho `sudoedit`.

~~~bash
sudo install -m 600 /dev/null /root/vpn20-relay-password
sudoedit /root/vpn20-relay-password
~~~

Trong trình sửa, nhập mật khẩu 12–256 ký tự vào một dòng, lưu và đóng tệp.
Không dùng lại đường dẫn trên nếu nó đang chứa tệp mật khẩu bạn cần giữ.
Sau đó tải bộ cài và dùng đường dẫn tệp làm giá trị của `--password-file`:

~~~bash
curl -fsSL --proto '=https' --proto-redir '=https' \
  https://raw.githubusercontent.com/lehuunghi/vpn/main/deploy/relay/install.sh \
  -o vpn20-relay-install.sh &&
sudo bash vpn20-relay-install.sh --domain relay.20.com.vn \
  --password-file /root/vpn20-relay-password &&
sudo rm -f /root/vpn20-relay-password
~~~

Nếu bộ cài lỗi, tệp được giữ để chạy lại; xóa tệp khi không còn cần dùng.
Khi cài trực tiếp trong SSH, dùng lệnh cài thông thường ở đầu trang để nhập ẩn;
không cần tạo tệp này.

Có thể dùng `--cert-file`, `--key-file` để dùng chứng chỉ do hệ thống hiện có
quản lý. Chứng chỉ cần được máy VPN20 tin cậy qua CA hệ thống; không bỏ xác minh
SSL hay tải CA từ một kết nối chưa được tin cậy. Chứng chỉ nguồn được đồng bộ
qua timer và lệnh `sudo vpn20-relay sync-tls`.

Nếu chọn `--control-port` khác 443, nhập `domain:cổng` trong VPN20 và mở cổng đó
trên firewall/security group. `--limit` đặt giới hạn mặc định cho máy mới; xem
[Đổi giới hạn tốc độ relay](#đổi-giới-hạn-tốc-độ-relay) để đổi cả máy đã đăng ký.
`--udp-start`/`--udp-end` đổi dải cổng mà không loại bỏ các cổng đã cấp.
Bộ cài không tự bật/tắt firewall hoặc thay đổi SSH.
