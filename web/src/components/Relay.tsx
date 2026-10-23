import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, type RelayStatus } from "../api";
import { errorMessage, useToast } from "../state";
import { Check, Field } from "./ui";

export function Relay() {
  const [status, setStatus] = useState<RelayStatus | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [domain, setDomain] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
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
      let profileZip: string | undefined;
      if (file) {
        if (file.size > 512 * 1024) throw new Error("ZIP profile tối đa 512 KB.");
        const bytes = new Uint8Array(await file.arrayBuffer());
        let binary = "";
        for (let i = 0; i < bytes.length; i += 8192) {
          binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
        }
        profileZip = btoa(binary);
      }
      const s = await api.saveRelay({ enabled, domain: domain.trim(), profileZip });
      setStatus(s);
      setEnabled(s.enabled);
      setDomain(s.domain);
      setFile(null);
      if (fileInput.current) fileInput.current.value = "";
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
        <Field label="Domain relay" hint="Nhập domain của VPS; để trống khi nhập ZIP để dùng domain trong profile.">
          <input aria-label="Domain relay" className="input" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="relay.20.com.vn"
            maxLength={253} autoCapitalize="none" autoCorrect="off" spellCheck={false} disabled={busy} />
        </Field>
        <Field label={status?.configured ? "Thay profile relay (tùy chọn)" : "Profile relay"}
          hint="Chọn ZIP do lệnh vpn20-relay add/export trên VPS tạo. Khóa và cổng được lấy tự động từ profile.">
          <input aria-label="ZIP profile relay" ref={fileInput} className="input" type="file" accept=".zip,application/zip"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)} disabled={busy} />
        </Field>
      </>}
      {status?.configured && <p className="muted">
        Profile: {status.profileName} · Endpoint: <span className="mono">{status.endpoint}</span>
      </p>}
      {(error || loadError) && <div className="error" role="alert">{error || loadError}</div>}
      <p role="status" aria-live="polite">{status?.message || "Đang tải cấu hình relay…"}</p>
      {status?.enabled && <p className="muted">Thiết bị đã cài cần tải lại .conf hoặc quét lại QR. Trang quản trị vẫn dùng cổng hiện tại.</p>}
      <button className="btn primary" type="submit" disabled={busy || !status}>{busy ? "Đang lưu…" : "Cập nhật relay"}</button>
    </div>
  </form>;
}
