import { useEffect, useState, type FormEvent } from "react";
import { api, type PublicAccessStatus } from "../api";
import { errorMessage, useToast } from "../state";
import { Check, Field } from "./ui";

const guide = "https://github.com/lehuunghi/vpn/blob/main/docs/public-access.md";

export function PublicAccess({ onEndpointChanged }: { onEndpointChanged: () => Promise<unknown> }) {
  const [status, setStatus] = useState<PublicAccessStatus | null>(null);
  const [subdomain, setSubdomain] = useState("");
  const [autoIPv4, setAutoIPv4] = useState(true);
  const [ipv4, setIPv4] = useState("");
  const [useEndpoint, setUseEndpoint] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();
  function load(next: PublicAccessStatus) {
    setStatus(next); setSubdomain(next.subdomain || ""); setAutoIPv4(next.autoIPv4); setIPv4(next.ipv4 || "");
  }
  useEffect(() => { api.publicAccess().then(load).catch((e) => setError(errorMessage(e))); }, []);
  async function act(action: "save" | "check" | "delete") {
    setBusy(true); setError("");
    try {
      const next = action === "save" ? await api.savePublicAccess({ subdomain, autoIPv4, ipv4, useEndpoint }) : action === "check" ? await api.checkPublicAccess() : await api.deletePublicAccess();
      load(next);
      if (action === "save" && useEndpoint) await onEndpointChanged();
      toast(action === "delete" ? "Đã gỡ bản ghi DNS. Hãy kiểm tra lại điểm kết nối VPN nếu đang dùng tên này." : "Đã đồng bộ DNS Cloudflare");
    } catch (e) {
      setError(errorMessage(e));
      try { load(await api.publicAccess()); } catch { /* Keep the original error. */ }
    } finally { setBusy(false); }
  }
  function submit(e: FormEvent) { e.preventDefault(); void act("save"); }
  return <section className="card" style={{ marginTop: 24 }}>
    <div className="card-head"><h2>Truy cập LAN và tên miền</h2></div>
    <div className="card-body stack">
      {status && <>
        <p>Địa chỉ lắng nghe: <code>{status.listen}</code>.</p>
        {status.loopbackOnly ? <div className="notice">Hiện chỉ truy cập được trên máy chủ. Để bật LAN trên Windows, chạy PowerShell với quyền Administrator: <code>&amp; "$env:ProgramFiles\VPN20\vpn20.exe" enable-lan</code>. <a href={`${guide}#truy-cập-trong-mạng-lan`} target="_blank" rel="noopener noreferrer">Hướng dẫn cho từng hệ điều hành</a></div> : <p>Truy cập từ máy cùng mạng bằng <code>http://IP-LAN-máy-chủ:51821</code> (dùng cổng và HTTPS tương ứng nếu đã đổi). Kiểm tra firewall TCP và địa chỉ LAN của máy chủ.</p>}
        {!!status.lanURLs?.length && <p className="hint">Địa chỉ trên máy chạy dịch vụ: {status.lanURLs.join(" · ")}. Với Docker, dùng IP của máy host.</p>}
      </>}
      <h3>Subdomain Cloudflare</h3>
      {status && !status.available && <div className="notice">{status.reason} <a href={guide} target="_blank" rel="noopener noreferrer">Cấu hình Cloudflare</a></div>}
      {status?.available && <form onSubmit={submit} className="stack">
        <div className="form-cols">
          <Field label="Subdomain" hint={`Chọn tên trong domain ${status.domain}.`}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}><input className="input" value={subdomain} onChange={(e) => setSubdomain(e.target.value)} pattern="[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?" maxLength={63} placeholder="vpn" disabled={busy || !!status.subdomain} required /><span>.{status.domain}</span></div>
          </Field>
          {!autoIPv4 && <Field label="IPv4 công khai"><input className="input mono" value={ipv4} onChange={(e) => setIPv4(e.target.value)} placeholder="IPv4 WAN của máy chủ/router" required disabled={busy} /></Field>}
        </div>
        <Check label="Tự nhận IPv4 công khai và cập nhật khi IP đổi" hint="Kiểm tra qua ipify và đồng bộ DNS mỗi 5 phút. Cần cho phép máy chủ kết nối HTTPS ra ngoài." checked={autoIPv4} onChange={setAutoIPv4} disabled={busy} />
        <Check label="Dùng subdomain này làm điểm kết nối VPN" hint="Áp dụng cho cấu hình tải về sau khi lưu. Cần chuyển tiếp cổng UDP của VPN trên router." checked={useEndpoint} onChange={setUseEndpoint} disabled={busy} />
        {status.hostname && <div className="notice" role="status">{status.registered ? "Đã có bản ghi DNS" : "Đang chờ đăng ký DNS"}: <strong>{status.hostname}</strong>{status.lastIP && ` → ${status.lastIP}`}{status.lastSync && ` · ${new Date(status.lastSync).toLocaleString("vi-VN")}`}</div>}
        <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
          <button className="btn primary" type="submit" disabled={busy}>{busy ? "Đang xử lý…" : "Lưu và đồng bộ DNS"}</button>
          {status.subdomain && <><button className="btn" type="button" disabled={busy} onClick={() => void act("check")}>Đồng bộ ngay</button><button className="btn danger" type="button" disabled={busy} onClick={() => { if (window.confirm("Gỡ bản ghi DNS do VPN20 tạo? Thiết bị dùng tên này sẽ cần đổi điểm kết nối.")) void act("delete"); }}>Gỡ subdomain</button></>}
        </div>
      </form>}
      {(error || status?.error) && <div className="error" role="alert">{error || status?.error}</div>}
      <p className="hint">DNS chỉ trỏ tên về IP công khai. Để mở trang quản trị qua Internet, cấu hình HTTPS bằng proxy hoặc dùng Cloudflare Tunnel. Mạng CGNAT có thể dùng Tunnel cho trang quản trị; WireGuard UDP cần IP/cổng công khai hoặc máy chủ trung gian. <a href={guide} target="_blank" rel="noopener noreferrer">Hướng dẫn từng bước</a></p>
    </div>
  </section>;
}
