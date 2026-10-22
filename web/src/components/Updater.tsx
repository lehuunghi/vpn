import { useEffect, useRef, useState } from "react";
import { ArrowUpCircle, RefreshCw } from "lucide-react";
import { api, type UpdateStatus } from "../api";
import { errorMessage } from "../state";

export function Updater() {
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [error, setError] = useState("");
  const [working, setWorking] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const initialVersion = useRef("");
  const hadUpdate = useRef(false);
  const activeJob = useRef("");
  useEffect(() => {
    let live = true;
    const refresh = () => api.updateStatus().then((s) => {
      if (!live) return;
      if (!initialVersion.current) initialVersion.current = s.current;
      if (s.busy) { hadUpdate.current = true; activeJob.current = s.job.id || ""; }
      if (hadUpdate.current && activeJob.current === s.job.id && !!s.job.version && !s.busy && s.job.phase === "complete") { window.location.reload(); return; }
      setStatus(s); setReconnecting(false);
    }).catch(() => { if (live) setReconnecting(true); });
    void refresh();
    const timer = setInterval(refresh, 3000);
    return () => { live = false; clearInterval(timer); };
  }, []);
  async function check() {
    setWorking(true); setError("");
    try { setStatus(await api.checkUpdate()); } catch (e) { setError(errorMessage(e)); } finally { setWorking(false); }
  }
  async function install() {
    hadUpdate.current = true;
    setWorking(true); setError("");
    try { const job = await api.startUpdate(); activeJob.current = job.id || ""; setStatus(await api.updateStatus()); } catch (e) { setError(errorMessage(e)); } finally { setWorking(false); }
  }
  const busy = working || !!status?.busy;
  return <section className="card" style={{ marginTop: 24 }}>
    <div className="card-head"><h2>Phiên bản và cập nhật</h2><span className="mono">{status?.current || "Đang tải…"}</span></div>
    <div className="card-body stack">
      <p>Cập nhật toàn bộ chương trình và giao diện. Thiết bị, khóa VPN, tài khoản và cài đặt được giữ lại. Kết nối VPN sẽ gián đoạn ngắn khi khởi động lại.</p>
      {status?.signing && <p className="hint">{status.signing.status === "Valid" ? `Chữ ký nhà phát hành hợp lệ: ${status.signing.publisher || "đã xác minh"}${status.signing.timestamped ? " · có dấu thời gian" : ""}.` : status.signing.status === "NotSigned" ? "Bản cài hiện tại chưa có chữ ký nhà phát hành. Cập nhật tự động chỉ nhận bộ cài được ký và tin cậy." : "Chưa xác minh được chữ ký nhà phát hành của bản cài hiện tại."}</p>}
      {status?.latest && <div className="notice">Có phiên bản <strong>{status.latest.version}</strong>{status.latest.prerelease && " · bản thử nghiệm"}. <a href={status.latest.url} target="_blank" rel="noopener noreferrer">Xem bản phát hành</a></div>}
      {status?.checkedAt && !status.latest && <p className="muted">Bạn đang dùng phiên bản mới nhất phù hợp với máy này.</p>}
      {status?.reason && <div className="notice">{status.reason} <a href="https://github.com/lehuunghi/vpn/blob/main/docs/installation.md#cập-nhật-từ-giao-diện-quản-trị" target="_blank" rel="noopener noreferrer">Hướng dẫn bật cập nhật</a></div>}
      {status?.job.message && <div className={status.job.phase === "failed" || status.job.phase === "rolled_back" ? "error" : "notice"} role="status">{status.job.message}</div>}
      {reconnecting && <p role="status">Đang kết nối lại với máy chủ… Trang sẽ tự nhận trạng thái khi dịch vụ hoạt động.</p>}
      {error && <div className="error">{error}</div>}
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
        <button type="button" className="btn" disabled={busy || !status} onClick={check}><RefreshCw size={16} /> Kiểm tra phiên bản</button>
        <button type="button" className="btn primary" disabled={busy || !status?.supported} onClick={install}><ArrowUpCircle size={16} /> {status?.busy ? "Đang cập nhật…" : "Cập nhật phiên bản mới nhất"}</button>
      </div>
      <p className="hint">Tự sao lưu trước khi cài và khôi phục khi bản mới không khởi động được. Windows chỉ chấp nhận bộ cài có chữ ký nhà phát hành và dấu thời gian hợp lệ.</p>
    </div>
  </section>;
}
