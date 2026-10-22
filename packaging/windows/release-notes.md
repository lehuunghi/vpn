## Cài VPN20 trên Windows

Tải **VPN20-1.0.0-windows.1-windows-x64-setup.exe** trong Assets, chạy bộ cài và chấp nhận quyền Administrator. Sau khi cài, mở **http://127.0.0.1:51821**, tạo tài khoản quản trị và nhập IP/tên miền công khai.

- Chạy trực tiếp trên Windows x64 bằng WireGuardNT; không cần Docker hoặc WSL.
- Dịch vụ **VPN20** tự chạy khi bật máy, trước khi đăng nhập; có tự khởi động lại nếu lỗi.
- Giữ giao diện tiếng Việt, quản lý thiết bị, cấu hình/QR, khóa chia sẻ trước, thống kê/lịch sử, tài khoản, 2FA, phân quyền, nhật ký và API.
- NAT IPv4 bằng WinNAT, cô lập thiết bị bằng Windows Filtering Platform, chỉnh MTU trực tiếp.
- Nâng cấp giữ cấu hình và dữ liệu. Gỡ cài đặt giữ dữ liệu để có thể cài lại.

**Yêu cầu:** Windows 10 1809+/11 Pro hoặc Enterprise x64, hoặc Windows Server 2019+; cần NetNat/WinNAT khả dụng và không có NAT khác đang chiếm WinNAT. Windows Home chưa được hỗ trợ. Mở/chuyển tiếp UDP 51820 từ router đến máy chạy VPN20 nếu máy nằm sau NAT.

**Khác biệt so với Linux:** Windows không có nftables/sysctl, không cung cấp NAT66 và không có điều chỉnh TCP MSS trong bản này. IPv6 có thể định tuyến với prefix/tuyến trả về phù hợp; dùng MTU để xử lý vấn đề kích thước gói. Không nên hiểu đây là cam kết tương đương 100% mọi chức năng mạng của Linux.

Đây là **bản phát hành thử nghiệm đầu tiên**. Quy trình phát hành kiểm tra trên Windows runner: WireGuardNT bắt tay và truyền UDP thật, WinNAT truy vấn DNS thật, cô lập hai thiết bị, bộ cài, giao diện/API, lưu dữ liệu, khởi động lại dịch vụ, nâng cấp và gỡ cài đặt. Chưa kiểm chứng khởi động lại toàn bộ máy hoặc mọi phiên bản Windows/phần mềm bảo mật ngoài thực tế.

Ứng dụng/bộ cài chưa ký chứng thư phát hành phần mềm; Windows có thể hiện cảnh báo nhà phát hành. Driver WireGuardNT đi kèm là bản chính thức có chữ ký và được kiểm tra SHA-256 trong CI. Đối chiếu **SHA256SUMS.txt** nếu cần.

Hướng dẫn: [docs/windows.md](https://github.com/lehuunghi/vpn/blob/main/docs/windows.md).
