import { useEffect, useState, type FormEvent } from "react";
import { api, type Settings } from "../api";
import { errorMessage, useAuth, useLive, useToast } from "../state";
import { Check, Field } from "../components/ui";
import { Updater } from "../components/Updater";

export function SettingsPage() {
  const { me } = useAuth();
  const { settingsVersion } = useLive();
  const toast = useToast();
  const [s, setS] = useState<Settings | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [platform, setPlatform] = useState("");
  const [backend, setBackend] = useState("");
  const [vpnRoutes, setVPNRoutes] = useState("");
  const readOnly = me?.role !== "admin";

  useEffect(() => {
    api.settings().then(setS).catch((e) => setError(errorMessage(e)));
    api.status().then((status) => { setPlatform(status.platform); setBackend(status.backend); setVPNRoutes([status.subnet4, status.subnet6].filter(Boolean).join(", ")); }).catch((e) => setError(errorMessage(e)));
  }, [settingsVersion]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!s) return;
    setError("");
    setBusy(true);
    try {
      setS(await api.saveSettings(s));
      toast("Đã lưu cài đặt");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (!s) return <div className="empty">{error || "Đang tải…"}</div>;
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setS({ ...s, [k]: v });

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Cài đặt</h1>
          <p>Thay đổi có hiệu lực ngay. Cấu hình thiết bị mới sử dụng giá trị mới; thiết bị đã cài cấu hình vẫn giữ giá trị cũ.</p>
        </div>
      </div>
      <form onSubmit={submit} className="stack">
        {error && <div className="error">{error}</div>}
        {platform === "windows" && <div className="notice">{backend === "relay" ? "Chuyển tiếp TCP/UDP và ping qua mạng máy chủ, hỗ trợ IPv4/IPv6 và điều chỉnh TCP MSS. Máy trong LAN nhận kết nối từ IP của máy chủ; không cần thêm tuyến VPN trên router. IPv6 ra ngoài cần kết nối IPv6 trên máy chủ." : "Chế độ WireGuardNT dùng WinNAT cho IPv4 và định tuyến IPv6. Chọn chế độ relay để chuyển tiếp qua mạng máy chủ và hỗ trợ điều chỉnh TCP MSS."}</div>}
        {readOnly && <div className="notice">Bạn có vai trò chỉ xem nên không thể chỉnh sửa cài đặt.</div>}
        <div className="card">
          <div className="card-head">
            <h2>Điểm kết nối</h2>
          </div>
          <div className="card-body">
            <div className="form-cols">
              <Field label="Tên miền hoặc IP công khai" hint="Tên miền hoặc địa chỉ IP mà thiết bị kết nối đến.">
                <input className="input" value={s.endpointHost} onChange={(e) => set("endpointHost", e.target.value)} disabled={readOnly} required />
              </Field>
              <Field label="Cổng công khai" hint="Cổng thiết bị kết nối đến. Thường là cổng lắng nghe; đổi giá trị nếu cổng UDP của container được ánh xạ lại.">
                <input className="input" type="number" min={1} max={65535} value={s.endpointPort} onChange={(e) => set("endpointPort", Number(e.target.value))} disabled={readOnly} required />
              </Field>
            </div>
          </div>
        </div>
        <div className="card">
          <div className="card-head">
            <h2>Mặc định cho thiết bị</h2>
          </div>
          <div className="card-body">
            <div className="form-cols">
              <Field label="DNS" hint="Phân tách bằng dấu phẩy. Thiết bị dùng các DNS này khi kết nối. Để trống nếu không cấp DNS.">
                <input className="input" value={s.dns} onChange={(e) => set("dns", e.target.value)} disabled={readOnly} />
              </Field>
              <Field label="Định tuyến thiết bị (AllowedIPs)" hint="0.0.0.0/0, ::/0 gửi toàn bộ lưu lượng qua VPN.">
                <input className="input mono" value={s.clientRoutes} onChange={(e) => set("clientRoutes", e.target.value)} disabled={readOnly} required />
                <button className="btn sm" type="button" disabled={readOnly || !vpnRoutes} onClick={() => setS({ ...s, clientRoutes: vpnRoutes, dns: "", peerIsolation: false })}>Chỉ mạng VPN · cho phép ping / remote</button>
                <button className="btn sm" type="button" disabled={readOnly} onClick={() => setS({ ...s, clientRoutes: "0.0.0.0/0, ::/0", dns: s.dns || "1.1.1.1, 1.0.0.1" })}>Toàn bộ lưu lượng qua VPN</button>
                <p className="small muted">Internet và DNS giữ theo mạng của thiết bị. Lưu cài đặt; với thiết bị cũ, vào Chỉnh sửa, xóa AllowedIPs và DNS để dùng mặc định mới, lưu rồi tải lại .conf.</p>
              </Field>
              <Field label="MTU" hint="Dùng 1420 cho mạng IPv4 có MTU 1500; 1412 cho PPPoE; 1400 hoặc thấp hơn cho IPv6 qua IPv6 hoặc khi tải xuống bị treo.">
                <input className="input" type="number" min={1280} max={9000} value={s.mtu} onChange={(e) => set("mtu", Number(e.target.value))} disabled={readOnly} required />
              </Field>
              <Field label="Duy trì kết nối định kỳ (giây)" hint="25 giây giúp giữ ánh xạ NAT trên bộ định tuyến gia đình. Đặt 0 để tắt.">
                <input className="input" type="number" min={0} max={65535} value={s.keepalive} onChange={(e) => set("keepalive", Number(e.target.value))} disabled={readOnly} required />
              </Field>
            </div>
            <Check label="Khóa chia sẻ trước" hint="Thêm khóa chia sẻ trước riêng cho mỗi thiết bị mới, bổ sung một lớp mã hóa đối xứng bên cạnh trao đổi khóa." checked={s.presharedKeys} onChange={(v) => set("presharedKeys", v)} disabled={readOnly} />
          </div>
        </div>
        <div className="card">
          <div className="card-head">
            <h2>Mạng</h2>
          </div>
          <div className="card-body">
            <Check label="Cô lập thiết bị" hint="Chặn lưu lượng giữa các thiết bị. Mỗi thiết bị có thể truy cập máy chủ và Internet, nhưng không truy cập thiết bị khác." checked={s.peerIsolation} onChange={(v) => set("peerIsolation", v)} disabled={readOnly} />
            <Check label="Điều chỉnh TCP MSS" hint={platform === "windows" && backend !== "relay" ? "Chọn chế độ relay để sử dụng chức năng này trên Windows." : "Điều chỉnh MSS của kết nối được chuyển tiếp cho phù hợp với MTU của VPN. Nên bật để tránh tình trạng đã kết nối nhưng trang web bị treo."} checked={s.clampMSS} onChange={(v) => set("clampMSS", v)} disabled={readOnly || (platform === "windows" && backend !== "relay")} />
            <Field label="Thời gian ghi nhận kết nối (giây)" hint="Thiết bị được xem là đang kết nối trong khoảng thời gian này kể từ lần bắt tay cuối. WireGuard từ chối phiên sau 180 giây.">
              <input className="input" type="number" min={30} max={3600} value={s.connectedWindow} onChange={(e) => set("connectedWindow", Number(e.target.value))} disabled={readOnly} required style={{ maxWidth: 160 }} />
            </Field>
          </div>
        </div>
        {!readOnly && (
          <div>
            <button className="btn primary" type="submit" disabled={busy}>
              {busy ? "…" : "Lưu cài đặt"}
            </button>
          </div>
        )}
      </form>
      {!readOnly && <Updater />}
    </>
  );
}
