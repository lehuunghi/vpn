# Truy cập quản trị bằng IP LAN

## Windows: cài là dùng được trong LAN

Từ bản 1.1.2, bộ cài Windows tự bật quản trị LAN trong cả cài mới, cài im
lặng và nâng cấp. Cập nhật bằng nút trong quản trị cũng chạy bộ cài này.
Không cần chạy thêm lệnh sau khi cài.

Bộ cài đổi listener thành `0.0.0.0:<cổng đang dùng>`, giữ các thiết lập khác,
khởi động dịch vụ và kiểm tra tình trạng. Windows Firewall mở đúng cổng
TCP, giới hạn nguồn kết nối ở **LocalSubnet**, áp dụng mọi network profile.
Nếu bật LAN không thành công, cấu hình cũ được khôi phục và bộ cài báo lỗi.

Sau khi cài xong, từ máy cùng mạng mở:

```text
http://192.168.1.100:51821
```

Thay `192.168.1.100` bằng IP LAN thật của máy chủ và dùng cổng đã cấu hình
nếu khác 51821. Nếu đã bật TLS, dùng `https://`. Đặt IP cố định hoặc DHCP
reservation để địa chỉ máy chủ không đổi.

`127.0.0.1` chỉ nhận kết nối từ chính máy chủ. `0.0.0.0` có nghĩa lắng nghe
trên các giao diện IPv4; đây không phải địa chỉ để nhập vào trình duyệt.
Máy ở VLAN/subnet khác cần quy tắc firewall cho subnet đó hoặc kết nối VPN.

### Kiểm tra khi không kết nối được

Trên máy chủ, mở PowerShell bằng Administrator:

```powershell
Get-Service VPN20
Get-NetTCPConnection -State Listen -LocalPort 51821
Get-NetIPAddress -AddressFamily IPv4
Get-NetFirewallRule -Name VPN20-Admin-LAN | Get-NetFirewallPortFilter
Get-NetFirewallRule -Name VPN20-Admin-LAN | Get-NetFirewallAddressFilter
Get-Content "$env:ProgramData\VPN20\vpn20.log" -Tail 60
```

Từ một máy khác trong LAN:

```powershell
Test-NetConnection 192.168.1.100 -Port 51821
```

Nếu listener là wildcard nhưng kết nối vẫn thất bại, kiểm tra IP, VLAN,
AP/client isolation, firewall của bên thứ ba và GPO có chặn local rules
hay không. `VPN20_MANAGE_FIREWALL=false` nghĩa host tự quản lý firewall.
Biến môi trường hệ thống được ưu tiên hơn JSON; nếu đã ghim listener trong
môi trường dịch vụ, cần sửa giá trị đó cho phù hợp.

Nếu TCP thông nhưng không đăng nhập được, kiểm tra HTTP/HTTPS và Secure
cookie. `VPN20_SECURE_COOKIES=true` yêu cầu dùng HTTPS để đăng nhập.

### Tùy chỉnh sau khi cài

Muốn giới hạn về một IP LAN cụ thể hoặc localhost, dừng dịch vụ, sửa
`C:\ProgramData\VPN20\config.json`, giữ các trường khác:

```json
"VPN20_HTTP_LISTEN": "192.168.1.100:51821"
```

Rồi `Start-Service VPN20`. Khởi động lại dịch vụ thông thường giữ listener
đã đặt; lần chạy bộ cài tiếp theo sẽ tự bật LAN theo yêu cầu triển khai.
Quy tắc quản trị được dọn khi dịch vụ dừng/gỡ cài. Gỡ ứng dụng giữ dữ liệu.

## Linux/systemd

Bản cài native mới mặc định `VPN20_HTTP_LISTEN=0.0.0.0:51821`. Với cấu hình
cũ đang dùng localhost, sửa `/etc/vpn20/vpn20.env` rồi chạy
`sudo systemctl restart vpn20`. Có thể dùng một IP LAN cụ thể để giới hạn
listener. Firewall INPUT của host/cloud do quản trị viên quản lý:

```sh
sudo ss -lntp 'sport = :51821'
# Ví dụ LAN 192.168.1.0/24; chỉ dùng lệnh của firewall thực tế:
sudo ufw allow from 192.168.1.0/24 to any port 51821 proto tcp
# Hoặc firewalld:
sudo firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="192.168.1.0/24" port port="51821" protocol="tcp" accept'
sudo firewall-cmd --reload
```

## Docker

Compose bridge mở TCP 51821 trên host. Có thể đặt trong `.env` để chỉ mở
trên một IP LAN:

```ini
VPN20_ADMIN_BIND=192.168.1.100
```

Chạy `docker compose -f docker-compose.yml up -d --no-build` để áp dụng port
mapping. Dùng IP host, không dùng IP container. Cập nhật image không tự đổi
mapping của container đã chạy; cần áp dụng lại Compose. Đặt
`VPN20_ADMIN_BIND=127.0.0.1` nếu chỉ dùng proxy trên host. Với
`docker-compose.host.yml`, đặt `VPN20_HTTP_LISTEN` trực tiếp cho mạng host.
Giữ volume dữ liệu khi tạo lại container.

Quản trị qua Internet cần HTTPS và quy tắc truy cập phù hợp; xem
[hướng dẫn cài đặt và HTTPS](installation.md#https-có-chuỗi-chứng-chỉ-đầy-đủ).
