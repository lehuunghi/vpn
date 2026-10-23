import { useEffect, useState, type FormEvent } from "react";
import { api, type RelayStatus } from "../api";
import { errorMessage, useToast } from "../state";
import { Check, Field } from "./ui";

export function Relay() {
  const [status, setStatus] = useState<RelayStatus | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [domain, setDomain] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  useEffect(() => {
    let alive = true;
    let initialized = false;
    const refresh = () => api.relay().then((s) => {
      if (!alive) return;
      setStatus(s);
      setLoadError("");
      if (!initialized) {
        setDomain(s.domain);
        setEnabled(s.enabled);
        initialized = true;
      }
    }).catch((e) => { if (alive) setLoadError(errorMessage(e)); });
    void refresh();
    const timer = setInterval(refresh, 3000);
    return () => { alive = false; clearInterval(timer); };
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const s = await api.saveRelay({ enabled, domain: domain.trim(), password: enabled && password ? password : undefined });
      setStatus(s);
      setEnabled(s.enabled);
      setDomain(s.domain);
      setPassword("");
      toast(s.enabled ? "Đã lưu relay. Tải lại cấu hình trên thiết bị sau khi kết nối." : "Đã tắt relay, dùng điểm kết nối trực tiếp.");
    } catch (e) {
      setError(errorMessage(e));
    } finally { setBusy(false); }
  }

  return <form className="card" onSubmit={submit} style={{ marginTop: 20 }}>
    <div className="card-head"><h2>Server relay</h2></div>
    <div className="card-body">
      <Check label="Dùng server relay" hint="Kết nối VPN qua VPS khi máy chủ sau NAT/CGNAT." checked={enabled} onChange={setEnabled} disabled={busy || !status} />
      {(enabled || status?.configured) && <>
        <Field label="Domain relay" hint="Nhập domain đã đặt khi cài relay trên VPS.">
          <input aria-label="Domain relay" className="input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="relay.20.com.vn"
            maxLength={259} autoCapitalize="none" autoCorrect="off" spellCheck={false} disabled={busy} />
        </Field>
        <Field label="Mật khẩu relay" hint={status?.configured ? "Đã lưu kết nối. Để trống để giữ nguyên; nhập mật khẩu khi đăng ký lại hoặc đổi VPS." : "Mật khẩu bạn tự đặt khi cài relay trên VPS. Không cần tải tệp cấu hình."}>
          <input aria-label="Mật khẩu relay" className="input" type="password" value={password}
            onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" maxLength={256} disabled={busy || !enabled} />
        </Field>
      </>}
      {status?.configured && <p className="muted">
        Điểm kết nối: <span className="mono">{status.endpoint}</span>
      </p>}
      {(error || loadError) && <div className="error" role="alert">{error || loadError}</div>}
      <p role="status" aria-live="polite">{status?.message || "Đang tải cấu hình relay…"}</p>
      {status?.enabled && <p className="muted">Thiết bị đã cài cần tải lại .conf hoặc quét lại QR. Trang quản trị vẫn dùng cổng hiện tại.</p>}
      <button className="btn primary" type="submit" disabled={busy || !status}>{busy ? "Đang lưu…" : "Cập nhật relay"}</button>
    </div>
  </form>;
}
