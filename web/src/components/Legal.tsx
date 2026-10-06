// Thông tin công ty và quyền truy cập mã nguồn.
// Ghi công nguồn gốc và giấy phép bên thứ ba được giữ trong NOTICE.
import { COMPANY_URL } from "../locale";

export function Legal({ center = false }: { center?: boolean }) {
  return (
    <p className={`legal${center ? " center" : ""}`}>
      <a href={COMPANY_URL} target="_blank" rel="noopener noreferrer">
        Công ty TNHH TN20 · 20.com.vn
      </a>
      <span className="legal-sep" aria-hidden="true">·</span>
      <a href="https://github.com/lehuunghi/vpn" target="_blank" rel="noopener noreferrer">
        Mã nguồn AGPL-3.0
      </a>
      <a href="https://github.com/lehuunghi/vpn/blob/main/NOTICE" target="_blank" rel="noopener noreferrer">
        Giấy phép và ghi công
      </a>
    </p>
  );
}
