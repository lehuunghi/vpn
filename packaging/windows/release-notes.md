## Windows 10/11: khởi động và VPN ngang hàng

Tải **VPN20-1.0.0-windows.2-windows-x64-setup.exe**, chạy với quyền Administrator để cài mới hoặc nâng cấp. Dữ liệu và cấu hình cũ được giữ lại. Mở **http://127.0.0.1:51821** sau khi cài.

- Sửa trường hợp khởi động thất bại khi thiếu nhà cung cấp WinNAT hoặc có NAT của ứng dụng khác: chế độ mặc định `auto` tiếp tục chạy VPN định tuyến, hiện cảnh báo NAT Internet trên bảng điều khiển. Không xóa NAT của ứng dụng khác.
- Bổ sung chế độ `routed` không cần WinNAT; `nat` vẫn có thể yêu cầu NAT bắt buộc. Lỗi driver/firewall/cô lập vẫn được báo và ngăn khởi động.
- Cài mới mặc định chỉ định tuyến subnet VPN, giữ Internet/DNS của máy khách. Với dữ liệu cũ, chọn **Cài đặt → Chỉ mạng VPN · cho phép ping / remote → Lưu**, kiểm tra giá trị riêng của từng thiết bị rồi tải lại cấu hình.
- Tải **Enable-VPN20-PeerAccess.ps1** và chạy trên từng máy Windows nhận kết nối bằng PowerShell quản trị. Script mở ping trên adapter VPN; thêm `-EnableRemoteDesktop` để bật RDP với NLA và quy tắc TCP/UDP giới hạn trong subnet VPN. `-Undo` hoàn tác các quy tắc và thiết lập RDP đã lưu. Script trong Assets mặc định subnet `10.8.0.0/24`; tải từ giao diện quản trị để lấy đúng subnet tùy chỉnh.
- Bộ cài hiển thị lỗi khởi động thực tế và phần cuối nhật ký. Có thể dừng/gỡ dịch vụ ngay cả khi tệp cấu hình bị lỗi.
- Dịch vụ tự chạy khi bật máy, trước đăng nhập, có cơ chế tự khởi động lại. Giữ các chức năng quản trị, QR, thống kê, 2FA, TLS và API từ bản trước.

Ví dụ sau khi hai máy đã kết nối WireGuard, máy đích có IP VPN `10.8.0.3`:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\Enable-VPN20-PeerAccess.ps1 -EnableRemoteDesktop
# Từ máy còn lại:
ping 10.8.0.3
mstsc /v:10.8.0.3
```

Windows Home có thể dùng VPN/ping và làm máy khách RDP; nhận Microsoft RDP cần Pro/Enterprise/Education/Server và tài khoản được phép có mật khẩu. VPN dùng IP định tuyến, không cung cấp tự khám phá máy như mạng LAN lớp 2. Máy chủ sau router vẫn cần chuyển tiếp UDP 51820; không cần mở cổng RDP ra Internet.

**Kiểm chứng:** CI trên Windows Server 2022 và 2025 chạy driver WireGuardNT thật, UDP, ICMP và TCP cổng 3389 giữa hai peer không NAT, cô lập WFP, DNS qua WinNAT; kiểm tra PowerShell 5.1, bộ cài, helper/hoàn tác, tự chạy dịch vụ, lưu dữ liệu, nâng cấp và gỡ. Chưa kiểm chứng trực tiếp trên mọi máy Windows 10/11, reboot toàn bộ máy hoặc đăng nhập RDP đồ họa.

Bản **prerelease**. Chưa có NAT66/viết lại TCP MSS như Linux; dùng định tuyến IPv6 và MTU phù hợp. Ứng dụng/bộ cài chưa ký chứng thư phát hành; driver WireGuardNT chính thức có chữ ký và SHA-256 cố định. Assets có **SHA256SUMS.txt**.

[Hướng dẫn đầy đủ](https://github.com/lehuunghi/vpn/blob/main/docs/windows.md).
