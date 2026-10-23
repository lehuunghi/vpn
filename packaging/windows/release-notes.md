# VPN20 1.2.2

- HTTPS tự động dùng chính cổng quản trị, mặc định https://ten.vpn.20.com.vn:51821. Gỡ listener HTTPS phụ, biến cổng phụ, mapping/EXPOSE Docker và provider DNS cũ không còn dùng.
- Sửa cấp/gia hạn SSL trong dịch vụ: không hỏi email qua stdin của Windows Service/Docker. Thực hiện ACME không tương tác và giữ lỗi gốc cho giao diện.
- Tạo/xóa TXT trực tiếp qua Worker; kiểm tra TXT công khai bằng DNS-over-HTTPS. Không phụ thuộc truy vấn SOA/NS trực tiếp tới DNS công khai trên cổng 53.
- Giao diện báo từng giai đoạn và mã lỗi SSL/CAA/TXT/CA/quyền lưu trữ; giữ nguyên nhân khi đang chờ tự thử lại. Không trả raw CA response, token hoặc HTML.
- Một socket phục vụ HTTPS và HTTP LAN/loopback để thiết lập/khôi phục. HTTP theo hostname/từ Internet chuyển hướng HTTPS sau khi hostname hoạt động; cookie HTTPS có Secure.
- Giữ hostname, khóa, chứng chỉ và DDNS khi SSL đang lỗi; Windows dọn rule HTTPS cũ và mở cổng quản trị khi chứng chỉ sẵn sàng.

**Nâng VPN20 lên 1.2.2, mở/chuyển tiếp TCP 51821 về máy chủ.**
Docker: bỏ mapping HTTPS phụ cũ, giữ 51821:51821 và UDP WireGuard rồi tạo lại container. Xóa biến VPN20_DDNS_HTTPS cũ nếu từng đặt; chỉ VPN20_HTTP_LISTEN quyết định cổng.
Worker đang chạy 0.2.1 giữ nguyên, không cần Deploy lại. API trung tâm ddns.20.com.vn vẫn dùng HTTPS 443.

Đã có kiểm thử ACME hoàn chỉnh bằng CA cục bộ: đăng ký tài khoản, tạo TXT, kiểm tra DNS qua HTTPS, xác nhận challenge, ký CSR, lưu/khôi phục chứng chỉ và dọn TXT; kiểm tra CAA error giữ đúng nguyên nhân. Không dùng CA/token Cloudflare thật trong kiểm thử.

Bộ cài Windows x64/x86, Linux amd64/arm64 và Docker. Không cung cấp Windows ARM64. Ứng dụng/bộ cài chưa ký mã; driver Wintun/WireGuardNT có chữ ký chính hãng. Có SHA-256 trong Assets.

Hướng dẫn: [subdomain/SSL](https://github.com/lehuunghi/vpn/blob/main/docs/subdomain.md) · [cài đặt](https://github.com/lehuunghi/vpn/blob/main/docs/installation.md).
