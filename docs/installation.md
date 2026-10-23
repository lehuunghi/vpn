# Cài đặt, chứng chỉ và cập nhật VPN20

VPN20 gồm máy chủ WireGuard, giao diện quản trị tiếng Việt và SQLite. Tất cả
thiết bị, khóa riêng, tài khoản/2FA, cài đặt và lịch sử nằm trong thư mục dữ
liệu. Nâng cấp phải giữ thư mục này. Không công khai cổng quản trị khi chưa
thiết lập tài khoản, HTTPS và quy tắc truy cập phù hợp.

## Chọn cách cài

| Máy chạy máy chủ | Cách cài | Ghi chú |
| --- | --- | --- |
| Windows 10 1607+/11, Server 2016–2025, Server Core | Bộ cài x64 hoặc x86 | Không cần Docker/WSL; x86 cho Windows 32-bit, x64 cho Intel/AMD 64-bit |
| Ubuntu/Debian, Rocky/Alma/RHEL, Fedora, openSUSE, Arch có systemd | Gói Linux amd64/arm64 và systemd | Cần root, nftables và WireGuard trong kernel hoặc wireguard-go |
| Linux có Docker Engine + Compose v2 | Docker Compose | Cách triển khai container chuẩn; giữ volume `/data` |
| Alpine Linux, NAS hoặc hệ không có systemd | Docker/Compose | Cần kernel/capability và kiến trúc được image hỗ trợ; tham khảo nhà cung cấp NAS |
| macOS hoặc Windows Docker Desktop/WSL2 | Docker Linux trong VM/WSL2 | Mạng UDP đi qua VM/NAT; để làm máy chủ ổn định nên dùng Linux VM hoặc bản Windows trực tiếp |
| Điện thoại, macOS, Linux, Windows dùng để kết nối | Ứng dụng WireGuard client | Quét QR hoặc nhập `.conf`; không cần chạy VPN20 trên mỗi client |

Bộ cài Windows ARM64 không được cung cấp. Windows 7/8/8.1 và Server 2012/R2
không đáp ứng phiên bản Go hiện tại. Docker/Linux vẫn giữ hỗ trợ amd64 và arm64.
Windows Home dùng được VPN và client RDP; nhận Microsoft RDP cần phiên bản
Windows có máy chủ RDP. Chưa kiểm thử trực tiếp mọi edition/thiết bị vật lý.

## Chuẩn bị mạng cho mọi cách cài

Để cấp tên **ten.vpn.20.com.vn** và SSL tự động chỉ bằng một nút trong
Cài đặt, xem [Subdomain và HTTPS tự động](subdomain.md). Tính năng dùng
cùng cổng quản trị **TCP 51821**: https://ten.vpn.20.com.vn:51821.
HTTP bằng IP LAN vẫn dùng để thiết lập/khôi phục; không cần cổng HTTPS phụ.

1. Đặt IP LAN cố định hoặc DHCP reservation cho máy chủ.
2. Chọn IP công khai/tên miền, ví dụ `vpn.example.com`; tên miền phải trỏ
   đúng về mạng máy chủ. VPN20 không cấp IP công khai. Xem
   [hướng dẫn truy cập LAN](lan-access.md).
3. Mở **UDP 51820** trên firewall của máy/nhà cung cấp VPS. Nếu sau router,
   chuyển tiếp UDP 51820 về IP LAN máy chủ; nhiều lớp NAT cần chuyển tiếp từng
   lớp. CGNAT cần IP công khai hoặc một đường chuyển tiếp UDP phù hợp.
4. Cổng quản trị mặc định **TCP 51821** dùng trên LAN; bản cài Windows mới
   tự giới hạn firewall theo LocalSubnet. Cổng HTTPS
   qua reverse proxy thường dùng TCP 443. Không chuyển tiếp cổng RDP 3389.
5. Tránh trùng subnet VPN `10.8.0.0/24` với LAN máy chủ và LAN các client.
   Nếu đổi subnet, dừng dịch vụ trước, sửa cấu hình rồi cấp lại `.conf`.
6. Tắt sleep/hibernate trên máy cần tiếp nhận VPN liên tục. Đồng bộ giờ hệ
   thống để bắt tay WireGuard, 2FA và kiểm tra chứng chỉ hoạt động đúng.

## Windows: cài trực tiếp

1. Mở [Releases](https://github.com/lehuunghi/vpn/releases), chọn bản Windows
   mới nhất có gói phù hợp:
   - `VPN20-<version>-windows-x64-setup.exe`: Windows Intel/AMD 64-bit.
   - `VPN20-<version>-windows-x86-setup.exe`: Windows 32-bit.
2. Kiểm tra checksum trong Assets; có thể xem thêm trạng thái chữ ký:

```powershell
Get-FileHash .\VPN20-<version>-windows-x64-setup.exe -Algorithm SHA256
Get-AuthenticodeSignature .\VPN20-<version>-windows-x64-setup.exe |
  Format-List Status,StatusMessage,SignerCertificate,TimeStamperCertificate
```

   So sánh SHA-256 với `SHA256SUMS.txt`. Bản 1.1.0 không ký mã ứng dụng/bộ cài,
   nên có thể hiện `NotSigned` hoặc Unknown publisher; không có chứng chỉ nhà
   phát hành công cộng đi kèm. Driver Wintun/WireGuardNT vẫn có chữ ký chính
   hãng riêng. Trình cập nhật nhận gói chưa ký từ release của dự án sau khi
   kiểm tra digest GitHub; nếu đã ghim chứng chỉ thì vẫn bắt buộc khớp chữ ký.
3. Chạy `.exe` bằng Administrator, làm theo cửa sổ cài đặt. Bộ cài thêm
   shortcut, driver, dịch vụ **VPN20** chạy dưới LocalSystem, tự chạy khi bật
   máy và tự khởi động lại khi lỗi. Từ 1.1.2, cài mới/nâng cấp đều tự bật LAN,
   giữ cổng đã cấu hình và thêm firewall TCP giới hạn LocalSubnet. Không cần
   chạy lệnh bật LAN sau cài đặt. Server Core có thể cài im lặng:

```powershell
.\VPN20-<version>-windows-x64-setup.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-
```

4. Mở shortcut VPN20 hoặc <http://127.0.0.1:51821> trên máy chủ; từ máy khác
   trong LAN mở `http://IP-LAN-máy-chủ:51821`. Tạo tài khoản quản trị
   đầu tiên, đặt endpoint công khai, tạo thiết bị, tải `.conf`/quét QR.
5. Dịch vụ, dữ liệu và chẩn đoán:

```powershell
Get-Service VPN20
Restart-Service VPN20
Get-Content "$env:ProgramData\VPN20\vpn20.log" -Tail 100
Get-NetAdapter -Name wg0
Get-NetFirewallRule -Group VPN20
```

Chương trình nằm trong `C:\Program Files\VPN20` (hoặc `Program Files (x86)`
cho x86). Dữ liệu/cấu hình nằm trong `C:\ProgramData\VPN20`; chỉ SYSTEM và
Administrators được đọc. Khi sửa `config.json`, dừng dịch vụ, giữ JSON hợp lệ,
rồi chạy lại. Xem [cấu hình Windows](windows.md#cấu-hình-và-dữ-liệu).

Mặc định **relay** cho phép TCP/UDP/ping ra LAN bằng IP máy chủ, không cần
WinNAT hoặc thêm tuyến VPN trên router. Trong Cài đặt, đặt AllowedIPs thành
`10.8.0.0/24, 192.168.1.0/24` để đến LAN ví dụ `192.168.1.0/24`, hoặc nhập
`0.0.0.0/0, ::/0` và DNS phù hợp để đưa toàn bộ lưu lượng qua VPN.
Sau thay đổi định tuyến/DNS, tải lại `.conf`
trên các client đã cài. Ping/RDP giữa peer cần tắt cô lập và cho phép dịch vụ
trên máy đích; có script tải ngay trong trang thiết bị. Xem [hướng dẫn RDP](windows.md#ping-và-remote-desktop-giữa-các-máy).

Nâng cấp thủ công: chạy bộ cài mới cùng kiến trúc. Không xóa ProgramData.
Gỡ cài đặt qua Settings → Apps hoặc trình gỡ đi kèm; dữ liệu được giữ lại.
Chỉ xóa dữ liệu sau khi đã sao lưu và chắc chắn không cần phục hồi thiết bị.

## Linux: gói native và systemd

Ví dụ dùng tài khoản có sudo. Cài công cụ hệ thống tương ứng:

```sh
# Ubuntu 22.04+/24.04, Debian 12/13
sudo apt-get update
sudo apt-get install -y ca-certificates curl python3 nftables wireguard-tools git

# Fedora, Rocky/Alma/RHEL 9+ (nếu kho hiện tại có wireguard-tools)
sudo dnf install -y ca-certificates curl python3 nftables wireguard-tools git

# Arch
sudo pacman -S --needed ca-certificates curl python nftables wireguard-tools git

# openSUSE Leap/Tumbleweed
sudo zypper install ca-certificates curl python3 nftables wireguard-tools git
```

Nhân Linux từ 5.6 thường có WireGuard; kiểm tra `sudo modprobe wireguard`.
Kernel của nhà cung cấp VPS có thể cần gói module riêng. Nếu kernel không hỗ
trợ, cài `wireguard-go` từ kho hệ điều hành rồi dùng `VPN20_BACKEND=userspace`.
Hệ không có systemd nên chọn Docker hoặc tự quản lý tiến trình; cập nhật một
chạm native chỉ dành cho dịch vụ systemd được cài ở vị trí chuẩn.

```sh
git clone --branch main https://github.com/lehuunghi/vpn.git
cd vpn
# Tải gói release phù hợp, xác minh SHA-256 do API GitHub cung cấp, rồi cài:
sudo bash packaging/linux/install.sh
# Hoặc chỉ định phiên bản Linux đã được phát hành:
sudo bash packaging/linux/install.sh --version 1.1.0
# Hoặc cài một gói đã tải/xây dựng và kiểm tra thủ công:
sudo bash packaging/linux/install.sh --archive /absolute/path/VPN20-<version>-linux-amd64.tar.gz
```

Không phải mọi tag Windows đều có gói Linux; chọn release chứa asset
`linux-amd64.tar.gz`/`linux-arm64.tar.gz`. Trình cài không chạy mã từ một URL
do người truy cập web cung cấp. Gói local qua `--archive` là lựa chọn tin cậy
của quản trị viên, cần tự kiểm tra nguồn và checksum trước khi dùng.

Các vị trí chuẩn:

| Thành phần | Vị trí |
| --- | --- |
| Chương trình và giao diện nhúng | `/usr/local/bin/vpn20` |
| Cấu hình hạ tầng | `/etc/vpn20/vpn20.env` (0600) |
| Database, TLS, bản sao lưu cập nhật | `/var/lib/vpn20` (0700) |
| Dịch vụ | `/etc/systemd/system/vpn20.service` |
| Nhật ký | `journalctl -u vpn20` |

```sh
sudo systemctl status vpn20
sudo journalctl -u vpn20 -n 100 --no-pager
sudoedit /etc/vpn20/vpn20.env
sudo systemctl restart vpn20
```

Cấu hình ví dụ cho LAN; dùng HTTPS/proxy khi mở quản trị qua Internet:

```ini
VPN20_DATA_DIR=/var/lib/vpn20
VPN20_HTTP_LISTEN=0.0.0.0:51821
VPN20_ENDPOINT=vpn.example.com
VPN20_PORT=51820
VPN20_SUBNET=10.8.0.0/24
VPN20_BACKEND=auto
VPN20_UPDATE_MODE=systemd
```

Mở `http://IP-LAN-máy-chủ:51821`; cho phép TCP 51821 từ đúng subnet LAN trong
firewall host. Có thể giữ listener `127.0.0.1` và dùng SSH tunnel:

```sh
ssh -L 51821:127.0.0.1:51821 user@server
```

Rồi mở <http://127.0.0.1:51821> trên máy của bạn. Mở UDP 51820 trong công cụ
firewall đang dùng, ví dụ `sudo ufw allow 51820/udp` hoặc
`sudo firewall-cmd --permanent --add-port=51820/udp && sudo firewall-cmd --reload`.
Chỉ dùng lệnh phù hợp với firewall thực tế; không tắt toàn bộ firewall.

Gỡ native: `sudo systemctl disable --now vpn20`, xóa unit và chương trình,
rồi `sudo systemctl daemon-reload`. Giữ `/var/lib/vpn20` và `/etc/vpn20` nếu
còn cần phục hồi. Không xóa database khi dịch vụ còn chạy.

## Docker Engine và Compose trên Linux

Cài Docker Engine và Compose v2 theo [hướng dẫn Docker](https://docs.docker.com/engine/install/)
cho bản phân phối đang dùng; xác minh `docker version` và `docker compose version`.
Host cần WireGuard trong kernel hoặc `/dev/net/tun` cho userspace. Container
cần `NET_ADMIN` và sysctl trong Compose; không dùng `--privileged`.

```sh
git clone --branch main https://github.com/lehuunghi/vpn.git
cd vpn
# Sửa VPN20_ENDPOINT trong docker-compose.yml
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d --no-build
docker compose -f docker-compose.yml logs -f --tail=100 vpn20
```

Image chính thức: `ghcr.io/lehuunghi/vpn20:latest`. `latest` theo lần build
thành công của main; dùng tag phiên bản/digest khi muốn ghim một bản đã biết.
Package GHCR phải public để máy không có tài khoản registry tải được. Xem
[quy trình GHCR](GHCR.md) nếu gặp denied/private hoặc cần tự build.

Compose bridge ánh xạ UDP 51820 và TCP 51821 trên host. Đặt `VPN20_ADMIN_BIND`
trong `.env` để chọn IP LAN cụ thể hoặc `127.0.0.1`. Volume `vpn20-data`
giữ `/data`; tên đầy đủ có thể kèm tiền tố project Compose. Không chạy
`docker compose down -v` nếu muốn giữ thiết bị, khóa và tài khoản.

### Bật cập nhật một chạm cho Docker

```sh
docker compose -f docker-compose.yml -f docker-compose.update.yml up -d --no-build
```

Override thêm Docker socket và chỉ định đúng container `vpn20`. Socket này
cấp quyền điều khiển Docker ở mức host cho tiến trình VPN20; chỉ bật trên
máy/quản trị viên được tin cậy. Giữ giao diện trên localhost/VPN hoặc HTTPS
với quyền truy cập phù hợp. Nếu không muốn cấp quyền này, giữ cấu hình chuẩn
và dùng cập nhật thủ công bên dưới; nút quản trị sẽ nêu rõ chưa bật khả năng tự cập nhật.

Lúc cập nhật, VPN20 đọc digest `latest` từ GHCR, pull đúng digest, tạo tác vụ
tạm từ image hiện tại, dừng container cũ, sao lưu DB, tạo container mới với
volume/cổng/env/capability/sysctl/mạng cũ và kiểm tra health. Tác vụ không có
kết nối mạng riêng và tự xóa sau khi kết thúc. Nếu bản mới lỗi, tự phục hồi container và database cũ. Khi bản mới đã khỏe,
xóa container cũ để không gây trùng service Compose; giữ image, metadata cấu
hình và bản sao DB để phục hồi thủ công. Volume dữ liệu không bị xóa.

Docker Engine/Compose một máy với mạng bridge/host được hỗ trợ. Container
có IP tĩnh hiện yêu cầu cập nhật qua Compose. Kubernetes, Swarm, Podman,
Watchtower hoặc hệ điều phối khác nên để hệ đó quản lý vòng đời; đừng để
nhiều công cụ cùng tự thay một container. Rootless Docker cần map đúng socket
vào `/var/run/docker.sock` và cấp quyền phù hợp; VPN NET_ADMIN còn tùy cấu hình host.

### Cập nhật Docker thủ công / ghim phiên bản

```sh
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d --no-build
docker compose -f docker-compose.yml logs --tail=100 vpn20
```

Nếu dùng override cập nhật, giữ cả hai `-f` khi chạy các lệnh Compose về sau.
Để ghim bản, sửa `image:` thành tag/digest cần dùng trước khi pull/up. Bản tự
build dùng `docker compose up -d --build`; gói build local không tự xuất hiện
trên GHCR cho tới khi workflow xuất bản thành công.

### Mạng host, Docker Desktop, NAS và IPv6

- Linux cần hiệu năng cao: dùng `docker-compose.host.yml`, bật forwarding và
  rp_filter theo [docs/performance.md](performance.md). Cổng quản trị dùng địa
  chỉ trong biến môi trường vì host mode không dùng `ports:`.
- Docker Desktop trên Windows/macOS: chọn **Linux containers**, bật WSL2 hoặc
  VM theo Docker; UDP phải đi xuyên NAT của Desktop/WSL và firewall host. Client
  VPN cần endpoint host/router thật, không phải IP riêng container. Bản Windows
  native tránh lớp mạng VM này.
- NAS: import Compose bằng công cụ của NAS, map volume bền vững, cho NET_ADMIN
  và kiểm tra kernel WireGuard/TUN. Không mặc định mọi NAS có đủ quyền này.
- IPv6: cấu hình subnet VPN6, bật forwarding IPv6 trên Linux/Compose và có
  upstream IPv6 hoạt động. Cần thêm AllowedIPs IPv6/DNS phù hợp trên client.
  Windows relay dùng IPv6 host; Linux/native dùng chính sách mạng đã cấu hình.

## Chứng chỉ: ký bộ cài và HTTPS là hai việc khác nhau

### Chữ ký nhà phát hành Windows

`AppPublisher`/logo không tạo được nhà phát hành được tin cậy. Cần chứng chỉ
**Code Signing** hợp lệ cùng khóa ký; chứng chỉ TLS website không thay thế được.
Bản 1.1.0 được phát hành không ký mã; không cần mua chứng chỉ để cài hoặc cập
nhật. Windows có thể hiện Unknown publisher. Nếu sau này bật ký, quy trình ký
SHA-256 và RFC 3161 timestamp cho `vpn20.exe`, Setup, Uninstall và các bản sao
tạm do Inno Setup tạo. Không ký lại driver của nhà
cung cấp. [Microsoft SignTool](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool)
và [Inno Setup SignTool](https://jrsoftware.org/ishelp/topic_setup_signtool.htm)
mô tả cách xác minh chữ ký và chuỗi tin cậy.

Cấu hình ký trong GitHub Actions → Settings → Secrets and variables → Actions:

- `VPN20_SIGN_PFX_BASE64`: PFX của chứng chỉ ký mã **được phép xuất khóa**, mã hóa base64.
- `VPN20_SIGN_PFX_PASSWORD`: mật khẩu PFX. Không commit PFX/khóa/mật khẩu vào repo.

Workflow chỉ cấp hai secret này khi build `main`. Nếu không cấu hình chứng
chỉ, CI vẫn kiểm tra, build và phát hành bộ cài unsigned. Khi ký local hoặc
trên runner/HSM riêng, cài khóa trong
`Cert:\CurrentUser\My`, đặt `VPN20_SIGN_CERTIFICATE_THUMBPRINT` rồi chạy build.
Chứng chỉ CA yêu cầu khóa phần cứng/cloud phải dùng phương thức ký mà CA cho
phép; không xuất khóa ra PFX nếu chứng chỉ/chính sách không cho phép. Có thể
thay tích hợp SignTool bằng provider HSM/cloud trên runner của bạn.

```powershell
$env:VPN20_REQUIRE_SIGNING = 'true'
$env:VPN20_SIGN_CERTIFICATE_THUMBPRINT = '<thumbprint-cua-chung-chi-ky-ma>'
.\packaging\windows\build.ps1 -Architecture x64
```

Có thể đặt `VPN20_SIGN_TIMESTAMP_URL` nếu dùng dịch vụ timestamp khác.
`Get-AuthenticodeSignature` phải có `Valid` trên máy nhận mà không thêm một
CA thử nghiệm tùy tiện. Chữ ký hợp lệ không đảm bảo SmartScreen luôn bỏ mọi
cảnh báo vì SmartScreen còn xét uy tín tệp/nhà phát hành. Nút cập nhật luôn
kiểm tra SHA-256 do API GitHub cung cấp trước khi chạy bộ cài. Không ghim chứng
chỉ thì nhận tệp unsigned; nếu tệp có chữ ký thì chữ ký/chuỗi/timestamp phải
hợp lệ. Đặt `VPN20_UPDATE_CERT_SHA1` để bắt buộc bộ cài có chữ ký từ đúng chứng
chỉ được ghim, từ chối cả tệp unsigned.

### HTTPS có chuỗi chứng chỉ đầy đủ

Để trình duyệt tin cậy, cần tên miền thuộc quyền quản lý và chứng chỉ CA hợp
lệ cho chính tên miền đó. `localhost`/IP LAN thường dùng CA nội bộ được quản
lý hoặc SSH tunnel; chứng chỉ tự ký không tự trở thành chứng chỉ công cộng.

Cách 1: VPN20 trực tiếp đọc **fullchain.pem** (leaf + intermediate) và private key:

```ini
VPN20_HTTP_LISTEN=0.0.0.0:51821
VPN20_TLS_CERT=/etc/letsencrypt/live/admin.example.com/fullchain.pem
VPN20_TLS_KEY=/etc/letsencrypt/live/admin.example.com/privkey.pem
```

Trên Windows, dùng đường dẫn tuyệt đối tới cert/key PEM và cấp quyền đọc cho
SYSTEM. Mở `https://admin.example.com:51821`; hostname phải khớp SAN của chứng
chỉ. Renew bằng công cụ CA đang dùng, sau đó restart dịch vụ để nạp cert mới.
Chỉ mở cổng quản trị cho các mạng cần dùng.

Cách 2: reverse proxy Caddy/Nginx quản lý ACME/Let's Encrypt ở cổng 443. VPN20
lắng nghe `127.0.0.1:51821`; proxy chuyển tiếp về đó. Ví dụ Caddyfile trên cùng
Linux host, với DNS và cổng 80/443 trỏ đúng về máy:

```caddyfile
admin.example.com {
    reverse_proxy 127.0.0.1:51821
}
```

Đặt `VPN20_SECURE_COOKIES=true` khi HTTPS kết thúc ở proxy và khai báo đúng
IP/CIDR proxy trong `VPN20_TRUSTED_PROXIES`. Không dùng `0.0.0.0/0` cho proxy
được tin cậy. Proxy Docker ở container khác cần cùng mạng hoặc đường host
phù hợp; `127.0.0.1` trong container proxy là chính container đó.

`VPN20_TLS_SELF_SIGNED=true` tạo cert lưu trong thư mục dữ liệu để mã hóa khi
thử nghiệm/quản trị nội bộ. Trình duyệt có thể cảnh báo cho tới khi CA/cert
được phân phối đúng theo chính sách của bạn; không cần vô hiệu hóa kiểm tra TLS.

## Cập nhật từ giao diện quản trị

Truy cập bằng IP LAN và kiểm tra firewall được hướng dẫn riêng tại
[hướng dẫn LAN](lan-access.md).

Đăng nhập bằng **admin** → **Cài đặt** → **Phiên bản và cập nhật**. Có thể bấm
**Kiểm tra phiên bản** để xem thông tin, hoặc bấm thẳng **Cập nhật phiên bản
mới nhất** để bắt đầu. Viewer không có quyền này. Trình duyệt không gửi URL
tải hay lệnh thực thi; máy chủ chỉ dùng repo/image cố định của dự án.

- Windows: dịch vụ cài bằng Setup, đủ quyền quản trị, release đúng x64/x86,
  digest GitHub khớp. Không bắt buộc chữ ký nếu chưa ghim chứng chỉ; có ghim thì
  bắt buộc chữ ký Authenticode/timestamp hợp lệ và đúng thumbprint.
- Linux: chương trình `/usr/local/bin/vpn20`, unit `vpn20.service`, root và
  systemd; tác vụ cập nhật chạy bằng transient unit để sống qua lúc dừng dịch vụ.
- Docker: cần override socket ở trên, volume dữ liệu riêng và healthcheck.
- Nhánh ổn định bỏ qua prerelease; bản đang chạy là prerelease tự tìm các
  prerelease phù hợp. `VPN20_UPDATE_PRERELEASE=true` cho phép chọn thử nghiệm.
  Không tự hạ phiên bản theo semver. Docker `latest` đi theo digest mới nhất
  từ GHCR, kể cả khi cùng nhãn phiên bản.

Trạng thái kiểm tra/tải/sao lưu/cài/khởi động/phục hồi hiển thị ngay trong
quản trị. Giao diện tự thử kết nối lại sau restart; kết nối VPN tạm ngắt.
Tài khoản, khóa, cấu hình và lưu lượng lịch sử vẫn được giữ trong DB/volume.
API ghi nhật ký ai bắt đầu cập nhật; hai cập nhật không chạy cùng lúc.

Các biến cấu hình bổ sung:

| Biến | Mặc định | Ý nghĩa |
| --- | --- | --- |
| `VPN20_UPDATE_MODE` | `auto` | `windows`, `systemd`, `docker`, hoặc `disabled` |
| `VPN20_UPDATE_PRERELEASE` | `false` | Cho phép release thử nghiệm; bản hiện tại prerelease cũng được xét |
| `VPN20_UPDATE_CERT_SHA1` | trống | Trống: nhận gói unsigned có digest đúng; đặt thumbprint để bắt buộc ký mã từ chứng chỉ đó |
| `VPN20_DOCKER_SOCKET` | `/var/run/docker.sock` | Socket Docker bên trong container |
| `VPN20_UPDATE_CONTAINER` | hostname container | Tên/ID chính container cần thay; override chuẩn dùng `vpn20` |

Bản sao lưu, kế hoạch và trạng thái nằm trong `<thư-mục-dữ-liệu>/updates`.
Windows có `update.log`/`installer.log`; Linux có journal của
`vpn20-update-<id>.service`. Giữ đủ dung lượng cho gói mới, bản chương trình
cũ và bản DB; sau khi xác nhận bản mới ổn định có thể lưu trữ/xóa các bản sao
lưu cũ bằng quyền quản trị. Đừng xóa tác vụ/bản sao lưu đang chạy.

Nếu mất điện/dừng cưỡng bức, kiểm tra trạng thái và nhật ký trước khi thử lại;
khóa tác vụ cũ quá 30 phút được ghi nhận là gián đoạn khi dịch vụ khởi động.
Không đảm bảo tự khôi phục được nếu hết đĩa, mất volume hoặc host Docker bị
tắt giữa thao tác. Có thể dùng bản sao lưu/cài lại gói trước để phục hồi.

## Sao lưu và phục hồi thủ công

Dừng dịch vụ/container trước khi sao chép toàn bộ thư mục dữ liệu để bao
gồm SQLite/WAL và TLS nhất quán. Giữ thêm cấu hình hạ tầng và phiên bản chương
trình/image. Bản sao lưu chứa khóa riêng và thông tin xác thực, cần lưu kín.

```sh
# Linux native
sudo systemctl stop vpn20
sudo tar -czf /root/vpn20-backup.tar.gz /var/lib/vpn20 /etc/vpn20
sudo systemctl start vpn20

# Docker: dừng rồi sao chép dữ liệu từ container, giữ container/volume
docker stop vpn20
docker cp vpn20:/data ./vpn20-backup-data
docker start vpn20
```

Windows: dừng VPN20, sao chép `C:\ProgramData\VPN20` vào vị trí chỉ quản trị
viên đọc được rồi chạy dịch vụ lại. Phục hồi bằng cách dừng dịch vụ, dùng đúng
phiên bản chương trình tương ứng và đưa dữ liệu trở lại vị trí cũ/đúng ACL.
Không chép đè DB lúc chương trình còn chạy.

## Tự build và kiểm tra

```sh
cd web && npm ci --ignore-scripts --no-audit --no-fund && npm run test:locale && npm run build && cd ..
go vet ./...
go test -count=1 ./...
bash packaging/linux/build.sh amd64
docker build -t vpn20:local .
```

Windows dùng PowerShell Administrator và `packaging/windows/build.ps1
-Architecture x64` hoặc `x86`. Build cần Go theo `go.mod`, Node 26, Inno Setup;
release có ký cần Windows SDK SignTool và chứng chỉ hợp lệ. CI kiểm thử packet
relay/native, Linux, installer Windows, API, giao diện và đóng gói. Kiểm thử
thực tế router, reboot, edition Windows cụ thể và phiên RDP đồ họa vẫn cần
thực hiện trên môi trường triển khai của bạn.
