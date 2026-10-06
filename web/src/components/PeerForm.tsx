import { useState, type FormEvent } from "react";
import { api, type Peer, type PeerInput, type Settings } from "../api";
import { fromLocalInput, toLocalInput } from "../format";
import { errorMessage } from "../state";
import { Check, Field, Modal } from "./ui";

// One form for create and edit. On create the key mode is chosen here; on
// edit keys and addresses are fixed (rotate keys from the detail view).
export function PeerForm({ peer, settings, onClose, onSaved }: { peer?: Peer; settings: Settings; onClose: () => void; onSaved: (p: Peer & { config?: string }) => void }) {
  const editing = !!peer;
  const [name, setName] = useState(peer?.name ?? "");
  const [keyMode, setKeyMode] = useState<"server" | "client">("server");
  const [publicKey, setPublicKey] = useState("");
  const [ipv4, setIpv4] = useState("");
  const [ipv6, setIpv6] = useState("");
  const [routes, setRoutes] = useState(peer?.clientRoutes ?? settings.clientRoutes);
  const [dns, setDns] = useState(peer?.dns ?? "");
  const [keepalive, setKeepalive] = useState(peer ? String(peer.keepalive) : "0");
  const [mtu, setMtu] = useState(peer ? String(peer.mtu) : "0");
  const [expires, setExpires] = useState(toLocalInput(peer?.expiresAt));
  const [notes, setNotes] = useState(peer?.notes ?? "");
  const [enabled, setEnabled] = useState(peer?.enabled ?? true);
  const [advanced, setAdvanced] = useState(editing);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    const body: PeerInput = {
      name,
      clientRoutes: routes,
      dns,
      keepalive: keepalive === "" ? null : Number(keepalive),
      mtu: mtu === "" ? null : Number(mtu),
      enabled,
      expiresAt: expires ? fromLocalInput(expires) : "1970-01-01T00:00:00Z",
      notes,
    };
    if (!editing) {
      if (keyMode === "client") body.publicKey = publicKey.trim();
      if (ipv4.trim()) body.ipv4 = ipv4.trim();
      if (ipv6.trim()) body.ipv6 = ipv6.trim();
    }
    try {
      const saved = editing ? await api.updatePeer(peer.id, body) : await api.createPeer(body);
      onSaved(saved);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={editing ? `Chỉnh sửa ${peer.name}` : "Thêm thiết bị"}
      onClose={onClose}
      footer={
        <>
          <button className="btn" type="button" onClick={onClose} disabled={busy}>
            Hủy
          </button>
          <button className="btn primary" type="submit" form="peer-form" disabled={busy}>
            {busy ? "…" : editing ? "Lưu" : "Tạo thiết bị"}
          </button>
        </>
      }
    >
      <form id="peer-form" onSubmit={submit}>
        {error && <div className="error">{error}</div>}
        <Field label="Tên" hint="Tên thiết bị hoặc người dùng, ví dụ: “Máy tính”, “Điện thoại”, “Bộ định tuyến văn phòng”.">
          <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} required maxLength={64} />
        </Field>
        {!editing && (
          <div className="field">
            <label>Khóa</label>
            <div className="btn-row">
              <label className="btn sm" style={{ cursor: "pointer" }}>
                <input type="radio" name="keys" checked={keyMode === "server"} onChange={() => setKeyMode("server")} /> Tạo tại đây (mã QR)
              </label>
              <label className="btn sm" style={{ cursor: "pointer" }}>
                <input type="radio" name="keys" checked={keyMode === "client"} onChange={() => setKeyMode("client")} /> Thiết bị cung cấp khóa công khai
              </label>
            </div>
            <span className="hint">Tạo khóa tại đây để sử dụng mã QR. Nếu thiết bị tự cung cấp khóa, khóa riêng luôn nằm trên thiết bị và sẽ không có mã QR.</span>
          </div>
        )}
        {!editing && keyMode === "client" && (
          <Field label="Khóa công khai của thiết bị">
            <input className="input mono" value={publicKey} onChange={(e) => setPublicKey(e.target.value)} placeholder="base64, 44 ký tự" required />
          </Field>
        )}
        {!advanced && (
          <button type="button" className="btn sm ghost" onClick={() => setAdvanced(true)} style={{ marginLeft: -8 }}>
            Tùy chọn nâng cao…
          </button>
        )}
        {advanced && (
          <>
            <Field label="Định tuyến thiết bị (AllowedIPs)" hint="Các mạng thiết bị gửi qua VPN. 0.0.0.0/0, ::/0 gửi toàn bộ lưu lượng; chỉ khai báo mạng VPN để định tuyến một phần.">
              <input className="input mono" value={routes} onChange={(e) => setRoutes(e.target.value)} />
            </Field>
            <div className="form-cols">
              <Field label="DNS" hint={`Để trống để dùng mặc định máy chủ (${settings.dns || "không có"}).`}>
                <input className="input" value={dns} onChange={(e) => setDns(e.target.value)} placeholder={settings.dns} />
              </Field>
              <Field label="Duy trì kết nối (giây)" hint={`Đặt 0 để dùng mặc định máy chủ (${settings.keepalive}).`}>
                <input className="input" type="number" min={0} max={65535} value={keepalive} onChange={(e) => setKeepalive(e.target.value)} />
              </Field>
              <Field label="MTU" hint={`Đặt 0 để dùng mặc định máy chủ (${settings.mtu}).`}>
                <input className="input" type="number" min={0} max={9000} value={mtu} onChange={(e) => setMtu(e.target.value)} />
              </Field>
              <Field label="Hết hạn" hint="Thiết bị sẽ bị ngắt kết nối vào thời điểm này. Để trống nếu không đặt thời hạn.">
                <input className="input" type="datetime-local" value={expires} onChange={(e) => setExpires(e.target.value)} />
              </Field>
              {!editing && (
                <>
                  <Field label="Địa chỉ IPv4" hint="Để trống để tự chọn địa chỉ còn trống.">
                    <input className="input mono" value={ipv4} onChange={(e) => setIpv4(e.target.value)} placeholder="tự động" />
                  </Field>
                  <Field label="Địa chỉ IPv6" hint="Chỉ dùng khi máy chủ có mạng IPv6.">
                    <input className="input mono" value={ipv6} onChange={(e) => setIpv6(e.target.value)} placeholder="tự động" />
                  </Field>
                </>
              )}
            </div>
            <Field label="Ghi chú">
              <textarea className="input" value={notes} onChange={(e) => setNotes(e.target.value)} maxLength={2000} />
            </Field>
            <Check label="Đã bật" hint="Thiết bị bị vô hiệu hóa sẽ được gỡ khỏi giao diện mạng và không thể kết nối." checked={enabled} onChange={setEnabled} />
          </>
        )}
      </form>
    </Modal>
  );
}
