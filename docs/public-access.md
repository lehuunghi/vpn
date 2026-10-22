# Truy cập LAN, subdomain Cloudflare và Internet

## Truy cập trong mạng LAN

`127.0.0.1` là địa chỉ vòng lặp: chỉ máy chủ tự kết nối được. Nếu VPN20 lắng
nghe `127.0.0.1:51821`, truy cập `192.168.1.100:51821` sẽ bị từ chối dù cả
hai địa chỉ thuộc cùng một máy. Mở firewall không thay đổi địa chỉ lắng nghe.

Từ bản 1.1.1, cài mới Windows/Linux và Compose bridge mặc định nhận kết nối
từ mạng. Truy cập `http://192.168.1.100:51821` bằng IP LAN thật của máy chủ.
`0.0.0.0`/`:51821` có nghĩa lắng nghe trên các giao diện, không phải địa chỉ
để nhập vào trình duyệt. Dữ liệu và địa chỉ lắng nghe đã cấu hình được giữ
khi nâng cấp; bản cũ cần bật LAN theo cách bên dưới.

### Windows

Nâng cấp bằng bộ cài mới rồi mở PowerShell **Run as administrator**:

```powershell
& "$env:ProgramFiles\VPN20\vpn20.exe" enable-lan
# Nếu cài bản x86 trên Windows 64-bit:
# & "${env:ProgramFiles(x86)}\VPN20\vpn20.exe" enable-lan
```

Lệnh đổi `VPN20_HTTP_LISTEN` thành `0.0.0.0:<cổng đang dùng>`, giữ các khóa
cấu hình khác và khởi động lại dịch vụ. Nếu không khởi động được, lệnh khôi
phục cấu hình cũ và thử chạy lại dịch vụ. Chạy từ bản đã cài, không chạy từ
một thư mục ZIP khác vì đường dẫn chương trình dịch vụ được cập nhật.

VPN20 thêm quy tắc **VPN20-Admin-LAN**: TCP theo đúng cổng đã cấu hình,
`RemoteAddress=LocalSubnet`, áp dụng mọi network profile. Quy tắc được dọn
khi dịch vụ dừng/gỡ cài. Khi chuyển về listener loopback, quy tắc LAN không
được thêm. `VPN20_MANAGE_FIREWALL=false` nghĩa quản trị viên tự mở firewall.
Máy ở VLAN/subnet khác cần quy tắc riêng cho đúng subnet hoặc dùng VPN.

Muốn chỉ lắng nghe một IP LAN, dừng dịch vụ, sửa
`C:\ProgramData\VPN20\config.json`, giữ các trường còn lại:

```json
"VPN20_HTTP_LISTEN": "192.168.1.100:51821"
```

Sau đó `Start-Service VPN20`. Đặt DHCP reservation để IP không đổi. Biến môi
trường hệ thống `VPN20_HTTP_LISTEN`, nếu có, ưu tiên hơn JSON; cần sửa/xóa
giá trị đó và khởi động lại dịch vụ/máy để môi trường dịch vụ được cập nhật.

Chẩn đoán trên máy chủ:

```powershell
Get-Service VPN20
Get-NetTCPConnection -State Listen -LocalPort 51821
Get-NetIPAddress -AddressFamily IPv4
Get-NetFirewallRule -Name VPN20-Admin-LAN | Get-NetFirewallAddressFilter
Get-Content "$env:ProgramData\VPN20\vpn20.log" -Tail 60
```

Chạy từ **máy khác trong LAN**:

```powershell
Test-NetConnection 192.168.1.100 -Port 51821
```

Nếu listener đã là wildcard nhưng TCP vẫn thất bại: kiểm tra IP đúng, VLAN,
AP/client isolation, phần mềm firewall và chính sách GPO chặn local rules.
Nếu TCP thông nhưng không đăng nhập được: kiểm tra HTTP/HTTPS và Secure
cookie. Khi bật `VPN20_SECURE_COOKIES=true`, dùng HTTPS để đăng nhập.

### Linux/systemd

Sửa `/etc/vpn20/vpn20.env`, đặt `VPN20_HTTP_LISTEN=0.0.0.0:51821`, rồi chạy
`sudo systemctl restart vpn20`. Hoặc dùng IP LAN cụ thể để giới hạn listener.

```sh
sudo ss -lntp 'sport = :51821'
# Ví dụ LAN 192.168.1.0/24; chỉ dùng lệnh của firewall thực tế trên máy:
sudo ufw allow from 192.168.1.0/24 to any port 51821 proto tcp
# Hoặc firewalld:
sudo firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="192.168.1.0/24" port port="51821" protocol="tcp" accept'
sudo firewall-cmd --reload
```

VPN20 quản lý NAT/chuyển tiếp VPN trên Linux; firewall INPUT của host, cloud
security group và router vẫn do quản trị viên quản lý.

### Docker

Compose bridge mới dùng `${VPN20_ADMIN_BIND:-0.0.0.0}:51821:51821/tcp`.
Đặt trong `.env` nếu muốn chỉ mở trên IP LAN:

```ini
VPN20_ADMIN_BIND=192.168.1.100
```

Chạy `docker compose -f docker-compose.yml up -d --no-build` để tạo lại port
mapping. Dùng IP **host**, không dùng IP container. Compose cũ đang chạy
không đổi mapping chỉ bằng việc cập nhật image; phải áp dụng lại Compose.
Muốn chỉ cho proxy trên host truy cập, đặt `VPN20_ADMIN_BIND=127.0.0.1`.
Với `docker-compose.host.yml`, sửa trực tiếp `VPN20_HTTP_LISTEN`; file host
vẫn giữ localhost mặc định. Không xóa volume dữ liệu khi tạo lại container.

## Chọn subdomain trong quản trị

Tính năng có sẵn trên Windows, Linux và Docker. Cần domain **bạn sở hữu**
đã hoạt động trên Cloudflare DNS. Ví dụ dưới đây dùng `example.com`; thay
bằng domain thật. VPN20 không cung cấp sẵn domain/IP công khai và không thể
tự dùng một domain chưa được chủ sở hữu cho phép.

1. Trong Cloudflare, mở domain, lấy **Zone ID** ở trang Overview.
2. Tạo API token qua **My Profile → API Tokens → Create Token → Edit zone
   DNS**. Chọn quyền **Zone → DNS → Edit** (API gọi là DNS Write), giới hạn
   **Include → Specific zone → domain của bạn**. Không cần Global API Key.
3. Lưu token vào file riêng trên máy chủ. Không gửi token qua chat, URL,
   ticket hoặc commit. Token này có quyền DNS của zone đã chọn; chỉ cấp cho
   người quản trị máy chủ đáng tin cậy.
4. Cấu hình domain, Zone ID và đường dẫn file token theo nền tảng bên dưới.
5. Khởi động lại VPN20. Đăng nhập admin → **Cài đặt → Truy cập LAN và tên
   miền → Subdomain Cloudflare**. Nhập `vpn-hanoi` để dùng
   `vpn-hanoi.example.com`. Chỉ cho phép một nhãn a-z, số và dấu gạch ngang.
6. Chọn nhận IPv4 tự động hoặc nhập IPv4 công khai. Bật **Dùng subdomain này
   làm điểm kết nối VPN** nếu muốn cấu hình thiết bị dùng tên đó. Bấm **Lưu
   và đồng bộ DNS**. Thiết bị đã nhập `.conf` cũ cần tải/nhập lại để đổi tên.

VPN20 tạo bản ghi **A, DNS only**, TTL 300 giây. Cloudflare proxy màu cam
không chuyển tiếp WireGuard UDP thông thường. Ở chế độ tự động, IPv4 được
kiểm tra qua HTTPS tới `api4.ipify.org` và đồng bộ mỗi 5 phút, kể cả sau khi
khởi động lại. Máy đi Internet qua một VPN/proxy khác có thể nhận IP của
đường đó; hãy dùng IP thủ công nếu khác WAN nhận kết nối vào.

VPN20 chỉ sửa/xóa bản ghi có dấu sở hữu riêng của máy này. Tên đã có bản
ghi của ứng dụng khác sẽ báo lỗi; không được ghi đè. Chọn tên mới hoặc tự
xử lý bản ghi cũ trong Cloudflare. **Gỡ subdomain** xóa bản ghi do VPN20 tạo;
không tự sửa endpoint đã lưu của thiết bị. Gỡ tên cũ trước khi đổi tên hoặc
đổi domain/zone. Token không được trả về API/trình duyệt hay lưu vào SQLite.

### Cấu hình Windows

Tạo file `C:\ProgramData\VPN20\cloudflare-token.txt` chứa duy nhất token,
không dấu ngoặc kép. Thư mục dữ liệu cài sẵn chỉ SYSTEM và Administrators
được truy cập. Thêm các trường sau vào `config.json`, giữ JSON hợp lệ:

```json
"VPN20_CLOUDFLARE_DOMAIN": "example.com",
"VPN20_CLOUDFLARE_ZONE_ID": "0123456789abcdef0123456789abcdef",
"VPN20_CLOUDFLARE_API_TOKEN_FILE": "C:\\ProgramData\\VPN20\\cloudflare-token.txt"
```

Chạy `Restart-Service VPN20`. Giữ file token khi nâng cấp/sao lưu, bảo vệ
quyền đọc tương tự database. Có thể dùng `VPN20_CLOUDFLARE_API_TOKEN` thay
file, nhưng không đặt đồng thời hai nguồn token.

### Cấu hình Linux/systemd

```sh
sudo install -m 0600 /dev/null /etc/vpn20/cloudflare-token
sudoedit /etc/vpn20/cloudflare-token
sudoedit /etc/vpn20/vpn20.env
```

Thêm vào file môi trường:

```ini
VPN20_CLOUDFLARE_DOMAIN=example.com
VPN20_CLOUDFLARE_ZONE_ID=0123456789abcdef0123456789abcdef
VPN20_CLOUDFLARE_API_TOKEN_FILE=/etc/vpn20/cloudflare-token
```

Chạy `sudo systemctl restart vpn20`.

### Cấu hình Docker

```sh
mkdir -p secrets
chmod 700 secrets
install -m 600 /dev/null secrets/cloudflare-api-token.txt
# Nhập token bằng trình soạn thảo, không ghi token vào lịch sử lệnh.
```

Thêm domain và Zone ID vào `.env`:

```ini
VPN20_CLOUDFLARE_DOMAIN=example.com
VPN20_CLOUDFLARE_ZONE_ID=0123456789abcdef0123456789abcdef
```

```sh
docker compose -f docker-compose.yml -f docker-compose.cloudflare.yml up -d --no-build
# Nếu dùng cả cập nhật một chạm, luôn giữ đủ các file override:
docker compose -f docker-compose.yml -f docker-compose.cloudflare.yml -f docker-compose.update.yml up -d --no-build
```

Token được mount bằng Compose secret tại `/run/secrets/cloudflare_api_token`.
Thư mục `secrets/` và `.env` đã bị loại khỏi git. Sau cập nhật một chạm,
mount/env được giữ; file secret trên host phải tiếp tục tồn tại.

## Đưa dịch vụ ra Internet

DNS thành công chỉ xác nhận tên đã trỏ về IP. Nó không tự cấu hình router,
gỡ CGNAT, mở firewall hoặc cấp chứng chỉ HTTPS cho chương trình.

### Có IPv4 công khai: WireGuard và HTTPS

1. Chuyển tiếp **UDP 51820** (hoặc cổng VPN thực tế) từ router tới máy chủ.
   Mở cùng cổng trên firewall/cloud security group. Đặt endpoint thiết bị
   thành `vpn-hanoi.example.com`, cổng công khai đúng với router.
2. Muốn mở trang quản trị, dùng HTTPS ở TCP 443. Ví dụ Caddy cùng host:

```caddyfile
vpn-hanoi.example.com {
    reverse_proxy 127.0.0.1:51821
}
```

3. Chuyển tiếp TCP 80/443 về Caddy để cấp/gia hạn chứng chỉ ACME theo cấu
   hình chuẩn. Cài Caddy từ nguồn chính thức, chạy bằng dịch vụ theo hướng
   dẫn của Caddy. DNS A phải trỏ đúng IP nhận các cổng đó; không để AAAA cũ
   trỏ về máy khác. TCP 443 và UDP 51820 dùng cùng hostname được.
4. Đặt `VPN20_SECURE_COOKIES=true`; nếu Caddy chạy cùng host, thêm
   `VPN20_TRUSTED_PROXIES=127.0.0.1/32,::1/128`. Khởi động lại VPN20. Dùng
   HTTPS để đăng nhập, kể cả trong LAN khi đã ép Secure cookie. Muốn tiếp
   tục LAN trực tiếp bằng IP, cấu hình HTTPS/certificate phù hợp cho IP đó.
5. Mở `https://vpn-hanoi.example.com` từ mạng di động để kiểm tra Internet.
   Router không hỗ trợ NAT loopback có thể cần DNS nội bộ trỏ domain về
   IP LAN. Không chuyển tiếp HTTP 51821 thẳng ra Internet.

Windows mặc định chỉ cho TCP 51821 từ LocalSubnet; reverse proxy cùng máy
vẫn truy cập được loopback. Cần thêm quy tắc firewall riêng cho TCP 80/443
của proxy. Nếu quản trị qua proxy hoàn toàn, có thể giữ listener localhost.
Tự ký TLS mã hóa nội bộ nhưng không tạo độ tin cậy công cộng như chứng chỉ CA.

### Không có IP công khai/CGNAT: Cloudflare Tunnel cho trang quản trị

Cloudflare Tunnel tạo kết nối ra ngoài từ `cloudflared`; không cần mở cổng
vào router để xem web. Dùng hostname riêng như `admin-hanoi.example.com`,
khác hostname A/DNS-only dùng làm endpoint WireGuard.

1. Tạo tài khoản quản trị VPN20 trên mạng tin cậy trước.
2. Trong Cloudflare dashboard, mở **Networking → Tunnels**, tạo tunnel
   Cloudflared. Cài connector theo lệnh dành cho Windows/Linux/Docker mà
   dashboard cung cấp. Giữ tunnel token riêng với DNS API token.
3. Thêm public hostname `admin-hanoi.example.com` và origin:
   - Connector chạy cùng Windows/Linux host: `http://127.0.0.1:51821`.
   - Connector trong Compose đi kèm: `http://vpn20:51821`.
   - Nếu VPN20 dùng TLS, cấu hình HTTPS origin và CA/hostname tin cậy tương
     ứng; không tắt kiểm tra chứng chỉ để né lỗi.
4. Windows/Linux native: cài `cloudflared` theo tài liệu chính thức, dùng
   lệnh `cloudflared service install <TUNNEL_TOKEN>` từ dashboard với quyền
   quản trị để tự chạy khi bật máy. Thêm `VPN20_SECURE_COOKIES=true` vào cấu
   hình VPN20 và restart; truy cập trang bằng URL HTTPS.
5. Docker: lưu tunnel token vào `secrets/cloudflare-tunnel-token.txt`. Image
   Cloudflared chạy bằng user 65532, nên file token cần đọc được trong
   container. Giữ thư mục `secrets` trên host ở mode 0700, rồi đặt riêng
   file tunnel token ở mode 0644; người dùng host khác vẫn không đi qua
   được thư mục 0700. Token DNS của VPN20 tiếp tục dùng mode 0600. Compose
   dùng bind mount cho secret dạng file, không tự ánh xạ lại uid/mode:

```sh
chmod 700 secrets
chmod 644 secrets/cloudflare-tunnel-token.txt
docker compose -f docker-compose.yml -f docker-compose.tunnel.yml up -d --no-build
# Kết hợp DDNS cho endpoint VPN và cập nhật nếu cần:
docker compose -f docker-compose.yml -f docker-compose.cloudflare.yml -f docker-compose.tunnel.yml -f docker-compose.update.yml up -d --no-build
```

6. Đợi tunnel Healthy rồi mở `https://admin-hanoi.example.com`. Có thể thêm
   chính sách Cloudflare Access giới hạn email/nhóm trước màn hình VPN20.
   Cấu hình TrustedProxies chỉ với IP/subnet connector tin cậy nếu cần IP
   client thật trong nhật ký; không tin cậy `0.0.0.0/0`.

Cloudflare Tunnel public hostname không cung cấp endpoint UDP tương thích
ứng dụng WireGuard thông thường. Muốn VPN dùng được sau CGNAT, cần mua IP
công khai từ ISP hoặc một VPS/relay có chuyển tiếp UDP và tuyến trả về.
Không đưa hostname Tunnel vào Endpoint WireGuard rồi kỳ vọng VPN bắt tay.
Các tuyến riêng qua Cloudflare One/WARP là một mô hình client khác.

## Khi cấp chung một domain cho nhiều máy chủ

Mỗi máy do bạn quản trị có thể dùng một tên riêng, ví dụ `hanoi.example.com`
và `saigon.example.com`. Không khôi phục cùng database/ownership sang hai
máy cùng hoạt động, vì hai bộ DDNS sẽ tranh cập nhật IP của một tên.

Nếu muốn mọi khách hàng tự chọn tên dưới domain của công ty mà không giữ
token DNS, cần dịch vụ cấp tên tập trung: xác thực máy, cấp một hostname
riêng, giới hạn đổi tên/IP và gọi Cloudflare từ phía dịch vụ. Không phát
token DNS toàn zone trong bộ cài cho khách hàng. Bản này dùng mô hình máy
chủ do quản trị viên tin cậy vận hành, chưa có dịch vụ cấp tên nhiều tenant.

## Tài liệu gốc

- [Cloudflare DNS API: tạo bản ghi](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/)
- [Cloudflare: DNS only và proxy](https://developers.cloudflare.com/dns/proxy-status/)
- [Cloudflare Tunnel: bắt đầu](https://developers.cloudflare.com/tunnel/get-started/)
- [Giao thức public hostname](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/protocols/)
- [Caddy HTTPS](https://caddyserver.com/docs/quick-starts/https)
- [Windows Firewall LocalSubnet](https://learn.microsoft.com/en-us/powershell/module/netsecurity/new-netfirewallrule)
- [Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/)
