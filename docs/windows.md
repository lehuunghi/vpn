# VPN20 cho Windows

VPN20 chạy máy chủ WireGuard và giao diện quản trị trực tiếp trên Windows,
không cần Docker hoặc WSL. Bộ cài có **x64** và **x86 (32-bit)**.
Mặc định dùng **relay**: wireguard-go, netstack và adapter **Wintun** chính thức.
TCP/UDP ra LAN hoặc Internet mở kết nối bằng IP của máy chủ; máy LAN trả lời
trực tiếp, không cần thêm tuyến VPN trên router và không phụ thuộc WinNAT.
Adapter VPN thật cho phép máy chủ chủ động ping/kết nối các thiết bị VPN.
**WireGuardNT** vẫn có trong gói để dùng chế độ native `routed` hoặc `nat`.
Bản Windows vẫn là prerelease; cần kiểm tra trên máy đích trước khi dùng sản xuất.

## Cài và sử dụng

1. Vào [Releases](https://github.com/lehuunghi/vpn/releases), mở bản Windows mới
   nhất và tải tệp `VPN20-…-windows-x64-setup.exe` hoặc `…-x86-setup.exe` đúng kiến trúc Windows trong **Assets**.
2. Chạy bộ cài với quyền Administrator. Bộ cài cài chương trình, driver và dịch
   vụ Windows **VPN20**, cấu hình tự chạy khi bật máy và khởi động dịch vụ.
3. Mở shortcut **VPN20** hoặc <http://127.0.0.1:51821>. Tạo tài khoản quản trị
   đầu tiên, nhập IP công khai hoặc tên miền của máy chủ.
4. Nếu máy nằm sau router, đặt IP LAN cố định và chuyển tiếp **UDP 51820** về
   máy. Nếu nhà mạng dùng CGNAT, cần IP công khai hoặc giải pháp chuyển tiếp
   UDP phù hợp. Bộ cài không thay cấu hình router của bạn.
5. Thêm thiết bị, quét QR bằng ứng dụng WireGuard hoặc tải cấu hình `.conf`.
   Kiểm tra bắt tay, truy cập mạng và lưu lượng trên bảng điều khiển.

Quản trị mặc định chỉ mở trên chính máy Windows. Cổng quản trị không được tự
mở ra Internet. Có thể bật HTTPS và đổi địa chỉ lắng nghe trong cấu hình.

## Yêu cầu và khác biệt nền tảng

- Windows 10 1607+ (build 14393+), Windows 11; Home/Pro/Enterprise/Education/LTSC.
  Windows Server 2016/2019/2022/2025 và Server Core. x86 dành cho Windows
  32-bit; x64 cho Intel/AMD 64-bit; Bộ cài Windows ARM64 không được cung cấp.
  Windows 7/8/8.1 và Server 2012/R2 nằm ngoài yêu cầu tối thiểu của Go hiện tại.
  Relay không yêu cầu WinNAT. Windows Home có thể nhận ping và dùng
  ứng dụng RDP để kết nối ra; nhận Microsoft RDP cần Pro/Enterprise/Education
  hoặc Server. CI kiểm tra Server 2022/2025 x64, x86 trên Server 2022 qua WOW64;
  chưa xác minh trực tiếp Windows 10 Home hoặc máy Windows 32-bit thật.
- Quyền Administrator để cài driver, dịch vụ và mạng. Dịch vụ chạy dưới
  `LocalSystem`, tự khởi động có độ trễ và tự chạy lại sau lỗi.
- Không cần cài riêng ứng dụng WireGuard lên máy chủ. Thiết bị kết nối vẫn cần
  ứng dụng/client WireGuard tương thích.
- Không để máy ngủ/hibernate nếu cần tiếp nhận VPN liên tục. VPN20 không thể
  chạy khi máy đang tắt hoặc ngủ.

| Chức năng | Bản Windows |
| --- | --- |
| Giao diện tiếng Việt, thiết bị, QR/cấu hình, thay khóa, thời hạn | Có, dùng cùng mã nguồn Linux |
| Tài khoản, 2FA/mã khôi phục, phân quyền, giới hạn đăng nhập, TLS | Có |
| Lưu lượng trực tiếp, lịch sử, nhật ký, health, Prometheus/API | Có |
| Mã hóa WireGuard | wireguard-go ở chế độ relay; WireGuardNT ở native |
| Ping, TCP/RDP giữa các IP VPN | Định tuyến, không cần WinNAT; tắt cô lập thiết bị |
| TCP/UDP ra LAN/Internet IPv4 | Relay dùng IP máy chủ, không cần WinNAT |
| Cô lập các thiết bị VPN | Relay lọc gói giữa peer; native dùng WFP; cả IPv4/IPv6 |
| IPv6 giữa các thiết bị và máy chủ | Có |
| TCP/UDP IPv6 ra ngoài | Relay dùng IPv6 của máy chủ; không cần tuyến trả về subnet VPN |
| Ping ra LAN/Internet IPv4/IPv6 | Relay gọi API ICMP Windows; chỉ trả lời khi đích thật phản hồi |
| Viết lại TCP MSS | Relay có cho SYN giữa peer; TCP ra ngoài tự thương lượng theo MTU |
| Linux sysctl/nftables | Không áp dụng; dùng API mạng Windows |
| Tự chạy khi bật máy, tự khôi phục dịch vụ | Có |

Mặc định `VPN20_WINDOWS_NETWORK_MODE=auto` và `VPN20_BACKEND=auto` chọn relay.
`relay` chọn tường minh cùng đường truyền; `userspace` cũng dùng relay trên
Windows. Không cần bật IP forwarding trên card LAN, RRAS, ICS hoặc WinNAT.
Relay chỉ mở cổng UDP WireGuard và cho phép lưu lượng vào adapter VPN;
không tự mở cổng quản trị ra mạng. Dịch vụ chỉ lắng nghe localhost vẫn riêng tư.
Chế độ này cần quyền quản trị để tạo Wintun; DLL chính thức đã được bộ cài kèm.

Để truy cập LAN, ví dụ LAN máy chủ là `192.168.1.0/24`, đặt AllowedIPs của
client thành `10.8.0.0/24, 192.168.1.0/24` (thêm subnet IPv6 nếu dùng), tải lại
cấu hình và kết nối. Hoặc bấm **Toàn bộ lưu lượng qua VPN** trong Cài đặt để
đưa cả Internet qua máy chủ. Cấu hình Windows mới vẫn mặc định chỉ tuyến VPN
và giữ DNS/Internet hiện tại của client; nâng cấp giữ cài đặt đã có.
Không cần tuyến VPN trên router LAN cho các kết nối TCP/UDP/ping do client mở.
Máy LAN cần cho phép dịch vụ từ IP LAN máy chủ. Máy ngoài VPN muốn chủ động
kết nối vào peer vẫn cần tuyến tới subnet VPN hoặc tham gia VPN.

IPv6 ra Internet cần kết nối IPv6 hoạt động trên máy chủ; relay không tạo
kết nối IPv6 khi upstream không có. Đây là chuyển tiếp ở mức TCP/UDP và ICMP
echo, không phải NAT66 ở mức gói: địa chỉ/port nguồn và phiên TCP có thể thay
đổi. GRE, ESP, multicast/broadcast và traceroute ra ngoài không được relay.
Gói giữa các peer đi nguyên giao thức qua VPN (có giảm TTL/hop limit); đây
vẫn là mạng lớp 3, không có khám phá LAN lớp 2. ICMP echo ra ngoài có giới
hạn 64 yêu cầu đồng thời; ICMP echo phân mảnh ra ngoài bị loại, không tạo
phản hồi giả. TCP/UDP phân mảnh được netstack tái lắp.

Relay giới hạn 4096 phiên TCP/UDP/ICMP đang xử lý, hàng đợi gói hữu hạn;
UDP đóng sau 2 phút không hoạt động, TCP sau 24 giờ không hoạt động. Ứng dụng
cần phiên TCP nhàn rỗi lâu nên gửi keepalive ở mức ứng dụng. Khóa/xóa/hết hạn
thiết bị loại peer khỏi WireGuard và đóng phiên relay của thiết bị đó.

Chế độ native: đặt `VPN20_WINDOWS_NETWORK_MODE=routed` hoặc `nat` với
`VPN20_BACKEND=auto/kernel`. `routed` giữ IP nguồn VPN, cần tuyến trả về khi
ra LAN; `nat` yêu cầu WinNAT IPv4. Với `VPN20_BACKEND=kernel` và mode `auto`,
thiếu WinNAT/NAT đang do Hyper-V, Docker hoặc HNS quản lý sẽ chuyển về routed
và hiện cảnh báo. NAT của ứng dụng khác được giữ nguyên. Native chưa có NAT66
hoặc MSS rewriting; tắt MSS trong giao diện trước khi chuyển relay sang native.
Relay khôi phục forwarding và xóa NAT VPN20 còn sót từ bản cũ khi khởi động.
Lỗi driver/firewall hoặc cô lập vẫn làm khởi động thất bại.

## Ping và Remote Desktop giữa các máy

1. Nâng cấp máy chủ bằng bộ cài mới. Trong **Cài đặt**, bấm **Chỉ mạng VPN ·
   cho phép ping / remote**, rồi **Lưu**. Nút này đặt AllowedIPs bằng subnet
   VPN, bỏ DNS riêng và tắt cô lập. Cài mới trên Windows mặc định dùng các
   tuyến VPN này. Dữ liệu cũ được giữ nguyên khi nâng cấp.
2. Kiểm tra từng thiết bị: bỏ giá trị DNS/AllowedIPs riêng cũ nếu cần. Tải lại
   `.conf`, nhập vào WireGuard và kết nối trên mỗi máy. Ví dụ AllowedIPs
   `10.8.0.0/24` cho phép đến tất cả IP trong subnet VPN; Internet vẫn đi qua
   mạng thông thường. Tránh subnet VPN trùng subnet LAN.
3. Trong phần tải cấu hình thiết bị, tải **Công cụ ping / Remote Desktop
   Windows**. Script tải từ giao diện dùng đúng subnet hiện tại; script trong
   Assets/ZIP mặc định `10.8.0.0/24`, có tham số `-VPNSubnet` để đổi.
4. Trên mỗi máy Windows cần nhận ping/kết nối, kết nối WireGuard rồi mở
   Windows PowerShell **Run as administrator**, chạy trong thư mục chứa script:

```powershell
# Cho phép nhận ping qua VPN
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Enable-VPN20-PeerAccess.ps1
# Cho phép ping và bật máy chủ RDP, yêu cầu NLA
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Enable-VPN20-PeerAccess.ps1 -EnableRemoteDesktop
# Chỉ định adapter nếu máy có nhiều tunnel
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Enable-VPN20-PeerAccess.ps1 -InterfaceAlias ten-tunnel -EnableRemoteDesktop
```

Script yêu cầu quyền quản trị, chỉ thêm quy tắc nhận ICMP/RDP trên adapter và
IP VPN từ subnet VPN; không tắt Windows Firewall. Quy tắc có sẵn của Windows
hoặc phần mềm khác giữ nguyên. RDP dùng cổng đã cấu hình và tài khoản Windows
được phép đăng nhập với mật khẩu; script không vượt chính sách quản trị.

Từ máy còn lại, ví dụ máy đích có IP VPN `10.8.0.3`:

```powershell
ping 10.8.0.3
mstsc /v:10.8.0.3
```

Không cần chuyển tiếp cổng RDP 3389 trên router. Đây là mạng IP định tuyến;
danh sách máy trong mục Network của Explorer không tự xuất hiện như LAN lớp 2.
Dùng IP VPN để remote hoặc truy cập dịch vụ. Để hoàn tác quy tắc và các thiết
lập RDP đã lưu, chạy script với `-Undo`. Nếu chỉ nhận ping trên Windows Home,
chạy không có `-EnableRemoteDesktop`.

## Cấu hình và dữ liệu

Chương trình nằm trong `C:\Program Files\VPN20`. Cấu hình hạ tầng nằm trong
`C:\ProgramData\VPN20\config.json`, cơ sở dữ liệu `vpn20.db`, TLS và nhật ký
`vpn20.log` nằm cùng thư mục. Thư mục dữ liệu chỉ cho SYSTEM và Administrators
truy cập vì cơ sở dữ liệu chứa khóa riêng và tài khoản.

Ví dụ cấu hình sau khi dừng dịch vụ:

```json
{
  "VPN20_BACKEND": "auto",
  "VPN20_HTTP_LISTEN": "127.0.0.1:51821",
  "VPN20_INTERFACE": "wg0",
  "VPN20_PORT": "51820",
  "VPN20_SUBNET": "10.8.0.0/24",
  "VPN20_WINDOWS_NETWORK_MODE": "auto",
  "VPN20_EGRESS_INTERFACE": "Ethernet"
}
```

Để trống hoặc bỏ `VPN20_EGRESS_INTERFACE` để tự chọn tuyến mặc định. Biến môi
trường `VPN20_*` được ưu tiên hơn tệp JSON. `VPN20_CONFIG` có thể chọn tệp cấu
hình khác cho chạy dòng lệnh. Dịch vụ cài sẵn dùng đường dẫn mặc định.
Các thiết lập thay đổi trực tiếp như endpoint, DNS, MTU và cô lập nằm trong
SQLite, chỉnh bằng giao diện như bản Linux. Không đổi subnet khi dịch vụ chạy.

PowerShell **Run as administrator**:

```powershell
Get-Service VPN20
Restart-Service VPN20
Get-Content "$env:ProgramData\VPN20\vpn20.log" -Tail 100
Get-NetNat
Get-NetAdapter -Name wg0
Get-NetFirewallRule -Group VPN20
& "$env:ProgramFiles\VPN20\vpn20.exe" reset-password admin
```

Nâng cấp bằng bộ cài mới; bộ cài dừng dịch vụ, giữ dữ liệu/cấu hình và khởi
động lại. Trước khi nâng cấp nên dừng dịch vụ và sao lưu **toàn bộ** thư mục dữ
liệu (gồm tệp SQLite WAL nếu có). Gỡ trong **Settings → Apps → VPN20**; quy tắc
mạng và dịch vụ bị gỡ, dữ liệu được giữ lại để cài lại. Xóa dữ liệu thủ công
khi chắc chắn không còn cần khóa và lịch sử.

Các lệnh `install-service`, `stop-service`, `uninstall-service` dùng để quản
lý khi cài thủ công từ ZIP. Chép toàn bộ ZIP vào thư mục chỉ quản trị viên có
quyền ghi, chép `config.example.json` vào đường dẫn cấu hình mặc định, rồi chạy
`vpn20.exe install-service` với quyền Administrator. Khuyến nghị dùng bộ cài.

## MTU, IPv6 và xử lý lỗi

- Nếu đã bắt tay nhưng trang web treo: thử MTU 1400 hoặc 1380, cập nhật cấu hình
  các thiết bị đã tạo. Relay có MSS; native cần chỉnh MTU.
- IPv6 trong tunnel dùng với `VPN20_SUBNET6`. Relay ra ngoài bằng IPv6 của
  máy chủ; native cần prefix và tuyến trả về. Không dùng WinNAT như NAT66.
- Bộ cài báo không khởi động được: hộp thoại hiển thị lỗi cụ thể và phần cuối
  nhật ký; xem thêm `install-error.txt` và `vpn20.log`. Kiểm tra JSON cấu hình,
  subnet trùng LAN, cổng UDP, quyền quản trị và phần mềm bảo mật chặn driver.
  Thiếu WinNAT không còn ngăn khởi động chế độ `auto`/`routed`. Nếu lỗi bind
  cổng quản trị vẫn còn sau 10 giây chờ, kiểm tra cổng TCP đang bị dùng hoặc
  Windows dành riêng; chọn `VPN20_HTTP_LISTEN` khác trong JSON rồi chạy lại
  bộ cài. Khi đổi cổng, mở trình duyệt bằng địa chỉ mới; shortcut vẫn trỏ
  đến cổng mặc định 51821.
- Lỗi mở UDP do cổng đang dùng hoặc Windows dành riêng: VPN20 chờ thử lại
  trong 10 giây rồi báo cổng cụ thể. Kiểm tra `netsh interface ipv4 show
  excludedportrange protocol=udp` và IPv6 tương ứng; chọn `VPN20_PORT` khác
  trong JSON, cập nhật cổng công khai của client và UDP forwarding trên router.
  Không xóa dải cổng hoặc NAT thuộc Hyper-V/HNS/Docker. Bộ cài chờ tối đa
  2 phút để driver/mạng hoàn tất trên máy cài lần đầu.
- Đã bắt tay nhưng không ping/remote: kiểm tra AllowedIPs, cô lập thiết bị và
  firewall của máy đích; chạy helper trên máy đích. RDP cần phiên bản Windows
  hỗ trợ nhận kết nối, dịch vụ TermService và tài khoản có quyền/mật khẩu.
- Máy chủ đã hoạt động nhưng từ ngoài không bắt tay: kiểm tra IP công khai,
  CGNAT, UDP forwarding và firewall upstream.
- Bộ cài/chương trình chưa được ký chứng thư phát hành; SmartScreen có thể
  cảnh báo nhà phát hành. Driver chính thức có chữ ký, không cần tắt kiểm tra
  chữ ký driver hoặc bật chế độ test signing.

## Biên dịch, kiểm chứng và phát hành

Trên máy Windows chạy PowerShell có quyền Administrator, cài Go theo
`go.mod`, Node 26, Git và Inno Setup 6, rồi chạy:

```powershell
./packaging/windows/build.ps1 -Architecture x64 # hoặc x86 trên runner phù hợp
./packaging/windows/smoke-install.ps1 -Installer (Get-ChildItem dist/release/*-setup.exe).FullName
```

`build.ps1` kiểm tra web, Go, tải Wintun 0.14.1 và WireGuardNT 1.1 với SHA-256
cố định và kiểm tra Authenticode. Các phép thử relay dùng hai client mã hóa
thật để kiểm tra TCP/UDP/ping IPv4/IPv6 giữa peer, máy chủ chủ động truy cập
peer qua Wintun, truy cập host bằng IP máy chủ, thống kê, MSS và cô lập. Các
phép thử netstack kiểm tra TCP half-close, UDP rỗng/lớn/phân mảnh, nhiều nguồn
cùng đích, thu hồi phiên và không giả phản hồi ping. API ICMP Windows được
kiểm tra qua IPv4/IPv6 loopback. Kiểm tra tích hợp tạo adapter thật, bắt tay và echo UDP với
hai client WireGuard trong bộ nhớ, truyền ICMP và TCP SYN/SYN-ACK cổng 3389
qua định tuyến không NAT, xác nhận cô lập WFP, truy vấn DNS qua WinNAT, đọc
thống kê và thêm/xóa thiết bị. Các ca PowerShell 5.1 mô phỏng nhà cung cấp
NetNat bị thiếu và NAT đang được dùng. Kiểm tra bộ cài xác nhận dịch vụ
automatic, giao diện/API, cấu hình, helper ping/RDP và hoàn tác, khởi động lại,
lưu dữ liệu, nâng cấp chế độ routed, mã thoát lỗi khi cổng quản trị bị chiếm,
khôi phục sau khi giải phóng cổng và gỡ. Phép thử TCP không xác minh phiên
đăng nhập RDP/đồ họa từ đầu đến cuối. Không chạy các phép thử tích hợp trên máy sản xuất.

Workflow `Windows release` chỉ phát hành sau khi kiểm tra Linux và Windows
thành công. Mỗi bản phát hành cần tăng `packaging/windows/VERSION`, cập nhật
release notes và đưa lên `main`. Assets gồm bộ cài `.exe` và ZIP cho từng kiến trúc, script ping/RDP và SHA-256.
Bản Windows là **prerelease** vì cần kiểm thử ngoài thực tế trên máy đích, qua
router và sau khởi động lại toàn bộ máy; CI kiểm tra cấu hình tự chạy và
restart dịch vụ, không reboot runner.

Nguồn kỹ thuật: [WireGuardNT](https://git.zx2c4.com/wireguard-nt/about/),
[WireGuard Windows](https://github.com/WireGuard/wireguard-windows),
[Microsoft WinNAT](https://learn.microsoft.com/windows-server/virtualization/hyper-v/setup-nat-network),
[Windows Filtering Platform](https://learn.microsoft.com/windows/win32/fwp/windows-filtering-platform-start-page).

[Microsoft: bật Remote Desktop](https://learn.microsoft.com/windows-server/remote/remote-desktop-services/remotepc/remote-desktop-allow-access),
[quy tắc firewall](https://learn.microsoft.com/powershell/module/netsecurity/new-netfirewallrule).

Yêu cầu nền tảng: [Go minimum requirements](https://go.dev/wiki/MinimumRequirements),
[Wintun chính thức và SHA-256](https://www.wintun.net/).

Hướng dẫn cài đặt đầy đủ, HTTPS, ký số và cập nhật một chạm: [docs/installation.md](installation.md).
