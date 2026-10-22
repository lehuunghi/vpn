# VPN20 cho Windows

VPN20 chạy máy chủ WireGuard và giao diện quản trị trực tiếp trên Windows x64,
không cần Docker hoặc WSL. Bản Windows dùng driver **WireGuardNT** chính thức.
Đây là bản thử nghiệm đầu tiên; cần thử trên máy đích trước khi dùng sản xuất.

## Cài và sử dụng

1. Vào [Releases](https://github.com/lehuunghi/vpn/releases), mở bản Windows mới
   nhất và tải tệp `VPN20-…-windows-x64-setup.exe` trong **Assets**.
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

- Windows 10 1809+ hoặc Windows 11 **Pro/Enterprise x64**, Windows Server
  2019/2022/2025. Yêu cầu nhà cung cấp **NetNat/WinNAT** hoạt động; Windows Home
  chưa được hỗ trợ. Phiên bản đầu được kiểm tra tự động trên `windows-latest`.
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
| Mã hóa WireGuard trong nhân | WireGuardNT |
| NAT ra Internet IPv4 | WinNAT |
| Cô lập các thiết bị VPN | WFP ở lớp IP forwarding, IPv4 và IPv6 |
| Định tuyến IPv6 | Có, cần prefix và tuyến trả về phù hợp |
| NAT66 như Linux | Chưa có |
| Viết lại TCP MSS như nftables | Chưa có; chỉnh MTU thay thế |
| Linux sysctl/nftables | Không áp dụng; dùng API mạng Windows |
| Tự chạy khi bật máy, tự khôi phục dịch vụ | Có |

WinNAT có giới hạn chia sẻ prefix. Nếu đã có NAT do Hyper-V, Docker, HNS hoặc
phần mềm khác quản lý, VPN20 báo lỗi và giữ nguyên NAT đó. **Không xóa NAT khác
một cách tự động.** Chọn máy chuyên dụng hoặc tự cấu hình định tuyến/NAT phù
hợp rồi đặt `VPN20_MANAGE_FIREWALL` thành `false`. Khi tắt quản lý firewall,
bạn cũng chịu trách nhiệm mở cổng, chuyển tiếp uplink và cô lập thiết bị.

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
  các thiết bị đã tạo. Windows không cung cấp tùy chọn viết lại TCP MSS ở đây.
- IPv6 trong tunnel dùng được với `VPN20_SUBNET6`; để IPv6 ra Internet cần
  prefix được định tuyến từ upstream và tuyến trả về. Không coi ULA + WinNAT là
  giải pháp NAT66. Không bật MSS clamp từ cơ sở dữ liệu cũ khi chuyển từ Linux.
- Bộ cài báo không khởi động được: xem `vpn20.log`; kiểm tra `Get-NetNat`,
  `Get-Command New-NetNat`, subnet trùng LAN, cổng UDP đang bị chiếm, phần mềm
  bảo mật chặn driver và kết nối uplink. VPN20 không tự tắt firewall khác.
- Máy chủ đã hoạt động nhưng từ ngoài không bắt tay: kiểm tra IP công khai,
  CGNAT, UDP forwarding và firewall upstream.
- Bộ cài/chương trình chưa được ký chứng thư phát hành; SmartScreen có thể
  cảnh báo nhà phát hành. Driver chính thức có chữ ký, không cần tắt kiểm tra
  chữ ký driver hoặc bật chế độ test signing.

## Biên dịch, kiểm chứng và phát hành

Trên máy Windows x64 chạy PowerShell có quyền Administrator, cài Go theo
`go.mod`, Node 26, Git và Inno Setup 6, rồi chạy:

```powershell
./packaging/windows/build.ps1
./packaging/windows/smoke-install.ps1 -Installer (Get-ChildItem dist/release/*-setup.exe).FullName
```

`build.ps1` kiểm tra web, Go, tải WireGuardNT 1.1 với SHA-256 cố định và kiểm
tra Authenticode. Kiểm tra tích hợp tạo adapter thật, bắt tay và echo UDP với
hai client WireGuard trong bộ nhớ, xác nhận cô lập WFP, truy vấn DNS qua
WinNAT, đọc thống kê và thêm/xóa thiết bị. Kiểm tra bộ cài xác nhận dịch vụ
automatic, trang web, tạo admin/thiết bị, QR/cấu hình, khởi động lại, lưu dữ
liệu, nâng cấp và gỡ. Không chạy các phép thử tích hợp trên máy sản xuất.

Workflow `Windows release` chỉ phát hành sau khi kiểm tra Linux và Windows
thành công. Mỗi bản phát hành cần tăng `packaging/windows/VERSION`, cập nhật
release notes và đưa lên `main`. Assets gồm bộ cài `.exe`, ZIP và SHA-256.
Bản đầu là **prerelease** vì cần kiểm thử ngoài thực tế trên máy đích, qua
router và sau khởi động lại toàn bộ máy; CI kiểm tra cấu hình tự chạy và
restart dịch vụ, không reboot runner.

Nguồn kỹ thuật: [WireGuardNT](https://git.zx2c4.com/wireguard-nt/about/),
[WireGuard Windows](https://github.com/WireGuard/wireguard-windows),
[Microsoft WinNAT](https://learn.microsoft.com/windows-server/virtualization/hyper-v/setup-nat-network),
[Windows Filtering Platform](https://learn.microsoft.com/windows/win32/fwp/windows-filtering-platform-start-page).
