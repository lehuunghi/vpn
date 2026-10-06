import { useEffect, useState } from "react";
import { Copy, Download, KeyRound, Pencil, Power, RefreshCw, Trash2 } from "lucide-react";
import { api, type Live, type Peer, type Range, type Settings, type TrafficPoint } from "../api";
import { ago, bytes, dateTime, duration, rate, shortKey } from "../format";
import { errorMessage, useNow, useToast } from "../state";
import { Legend, TrafficChart } from "./charts";
import { Confirm, Modal, Segmented, copyText } from "./ui";
import { PeerForm } from "./PeerForm";

const rangeMs: Record<Range, number> = { "1h": 3600e3, "24h": 86400e3, "7d": 7 * 86400e3, "30d": 30 * 86400e3 };

export function PeerDetail({ peer, live, settings, isAdmin, onClose, onChanged, initialTab = "overview", initialConfig }: { peer: Peer; live: Live; settings: Settings; isAdmin: boolean; onClose: () => void; onChanged: (p?: Peer) => void; initialTab?: "overview" | "config"; initialConfig?: string }) {
  const toast = useToast();
  const now = useNow(1000);
  const [tab, setTab] = useState<"overview" | "config">(initialTab);
  const [config, setConfig] = useState<string>(initialConfig ?? "");
  const [range, setRange] = useState<Range>("24h");
  const [series, setSeries] = useState<TrafficPoint[]>([]);
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<null | "delete" | "rotate" | "disable">(null);
  const [busy, setBusy] = useState(false);
  const [qrKey, setQrKey] = useState(0);

  useEffect(() => {
    if (tab === "config" && !config) api.peerConfig(peer.id).then(setConfig).catch((e) => toast(errorMessage(e), "bad"));
  }, [tab, config, peer.id, toast]);
  useEffect(() => {
    api.peerUsage(peer.id, range).then(setSeries).catch(() => {});
  }, [peer.id, range, peer.updatedAt]);

  async function act(fn: () => Promise<unknown>, done: string) {
    setBusy(true);
    try {
      await fn();
      toast(done);
      onChanged();
    } catch (e) {
      toast(errorMessage(e), "bad");
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  }

  const state = !peer.enabled ? "disabled" : peer.expired ? "expired" : live.connected ? "on" : "off";
  const stateLabel = { disabled: "Đã vô hiệu hóa", expired: "Đã hết hạn", on: "Đang kết nối", off: "Chưa kết nối" }[state];

  return (
    <>
      <Modal
        title={peer.name}
        onClose={onClose}
        wide
        footer={
          isAdmin ? (
            <div className="btn-row" style={{ justifyContent: "flex-end", width: "100%" }}>
              <button className="btn sm" onClick={() => setEditing(true)} disabled={busy}>
                <Pencil /> Chỉnh sửa
              </button>
              {peer.enabled ? (
                <button className="btn sm" onClick={() => setConfirm("disable")} disabled={busy} title="Gỡ khỏi giao diện mạng và ngắt phiên kết nối">
                  <Power /> Ngắt kết nối
                </button>
              ) : (
                <button className="btn sm" onClick={() => act(() => api.enablePeer(peer.id), "Đã bật thiết bị")} disabled={busy}>
                  <Power /> Bật
                </button>
              )}
              <button className="btn sm" onClick={() => act(() => api.resetPeer(peer.id), "Đã đặt lại phiên kết nối")} disabled={busy || !peer.enabled} title="Ngắt phiên hiện tại; thiết bị đang truyền dữ liệu sẽ thiết lập lại kết nối sau khoảng 15 giây">
                <RefreshCw /> Đặt lại phiên
              </button>
              {peer.serverKeys && (
                <button className="btn sm" onClick={() => setConfirm("rotate")} disabled={busy} title="Tạo cặp khóa mới; cấu hình cũ sẽ ngừng hoạt động">
                  <KeyRound /> Thay cặp khóa
                </button>
              )}
              <button className="btn sm danger" onClick={() => setConfirm("delete")} disabled={busy}>
                <Trash2 /> Xóa
              </button>
            </div>
          ) : undefined
        }
      >
        <div className="tabs">
          <button className={tab === "overview" ? "active" : ""} onClick={() => setTab("overview")}>
            Tổng quan
          </button>
          <button className={tab === "config" ? "active" : ""} onClick={() => setTab("config")}>
            Cấu hình
          </button>
        </div>
        {tab === "overview" && (
          <>
            <div className="grid grid-2">
              <dl className="kv">
                <dt>Trạng thái</dt>
                <dd>
                  <span className={`dot ${state}`} />
                  {stateLabel}
                  {live.connected && live.connectedSince && <span className="faint"> trong {duration(live.connectedSince, now)}</span>}
                </dd>
                <dt>Địa chỉ VPN</dt>
                <dd className="mono">
                  {peer.ipv4}
                  {peer.ipv6 ? `, ${peer.ipv6}` : ""}
                </dd>
                <dt>Điểm kết nối</dt>
                <dd className="mono">{live.endpoint || "—"}</dd>
                <dt>Bắt tay gần nhất</dt>
                <dd>
                  {ago(live.lastHandshake, now)} <span className="faint">{dateTime(live.lastHandshake)}</span>
                </dd>
                <dt>Tốc độ</dt>
                <dd className="num">
                  ↓ {rate(live.rxRate)} · ↑ {rate(live.txRate)}
                </dd>
                <dt>Dữ liệu truyền</dt>
                <dd className="num">
                  ↓ {bytes(live.rx)} · ↑ {bytes(live.tx)}
                </dd>
              </dl>
              <dl className="kv">
                <dt>Khóa công khai</dt>
                <dd className="mono" title={peer.publicKey}>
                  {shortKey(peer.publicKey)}{" "}
                  <button className="btn icon ghost sm" title="Sao chép" onClick={() => copyText(peer.publicKey).then((ok) => toast(ok ? "Đã sao chép" : "Không thể sao chép", ok ? "ok" : "bad"))}>
                    <Copy />
                  </button>
                </dd>
                <dt>Khóa</dt>
                <dd>
                  {peer.serverKeys ? "do máy chủ tạo" : "do thiết bị giữ"}
                  {peer.presharedKey ? " · khóa chia sẻ trước" : ""}
                </dd>
                <dt>Định tuyến thiết bị</dt>
                <dd className="mono">{peer.clientRoutes}</dd>
                <dt>DNS</dt>
                <dd>{peer.dns || <span className="faint">mặc định máy chủ ({settings.dns || "không có"})</span>}</dd>
                <dt>Duy trì kết nối / MTU</dt>
                <dd>
                  {peer.keepalive || settings.keepalive} giây / {peer.mtu || settings.mtu}
                </dd>
                <dt>Hết hạn</dt>
                <dd>{peer.expiresAt ? dateTime(peer.expiresAt) : <span className="faint">không hết hạn</span>}</dd>
                <dt>Ngày tạo</dt>
                <dd>{dateTime(peer.createdAt)}</dd>
              </dl>
            </div>
            {peer.notes && <p className="mt muted" style={{ whiteSpace: "pre-wrap" }}>{peer.notes}</p>}
            <div className="mt" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <Legend />
              <Segmented value={range} onChange={setRange} options={[{ value: "1h", label: "1 giờ" }, { value: "24h", label: "24 giờ" }, { value: "7d", label: "7 ngày" }, { value: "30d", label: "30 ngày" }]} />
            </div>
            <TrafficChart points={series} from={now - rangeMs[range]} to={now} bucketSeconds={range === "30d" ? 3600 : 300} />
          </>
        )}
        {tab === "config" && (
          <div className="qr">
            {peer.serverKeys ? (
              <img key={qrKey} src={`/api/peers/${peer.id}/qr.png?size=384&v=${peer.updatedAt}`} alt="Mã QR cấu hình thiết bị" width={320} height={320} onError={() => setQrKey((k) => k + 1)} />
            ) : (
              <div className="notice">Thiết bị này tự giữ khóa riêng nên không có mã QR. Hãy điền dòng PrivateKey trên thiết bị.</div>
            )}
            <pre className="config">{config || "…"}</pre>
            <div className="btn-row">
              <button className="btn" onClick={() => copyText(config).then((ok) => toast(ok ? "Đã sao chép cấu hình" : "Không thể sao chép", ok ? "ok" : "bad"))} disabled={!config}>
                <Copy /> Sao chép
              </button>
              <a className="btn" href={`/api/peers/${peer.id}/config?download=1`}>
                <Download /> Tải tệp .conf
              </a>
              <span className="small faint">Bất kỳ ai có tệp này đều có thể kết nối với danh tính của thiết bị. Việc xem cấu hình được ghi trong nhật ký.</span>
            </div>
          </div>
        )}
      </Modal>
      {editing && (
        <PeerForm
          peer={peer}
          settings={settings}
          onClose={() => setEditing(false)}
          onSaved={(p) => {
            setEditing(false);
            setConfig("");
            toast("Đã lưu thiết bị");
            onChanged(p);
          }}
        />
      )}
      {confirm === "delete" && <Confirm title="Xóa thiết bị" danger confirmLabel="Xóa" busy={busy} onClose={() => setConfirm(null)} onConfirm={() => act(() => api.deletePeer(peer.id).then(() => onClose()), "Đã xóa thiết bị")} text={<>Xóa <b>{peer.name}</b>? Khóa, địa chỉ và lịch sử lưu lượng sẽ bị xóa vĩnh viễn.</>} />}
      {confirm === "disable" && <Confirm title="Ngắt kết nối thiết bị" confirmLabel="Ngắt kết nối" busy={busy} onClose={() => setConfirm(null)} onConfirm={() => act(() => api.disablePeer(peer.id), "Đã ngắt kết nối thiết bị")} text={<>Gỡ <b>{peer.name}</b> khỏi giao diện mạng? Phiên kết nối sẽ bị ngắt ngay và thiết bị chỉ có thể kết nối lại khi được bật.</>} />}
      {confirm === "rotate" && (
        <Confirm
          title="Thay cặp khóa"
          confirmLabel="Thay khóa"
          busy={busy}
          onClose={() => setConfirm(null)}
          onConfirm={() =>
            act(
              () =>
                api.rotatePeer(peer.id).then((p) => {
                  setConfig(p.config);
                  setTab("config");
                }),
              "Đã thay khóa; hãy cấp cấu hình mới cho thiết bị",
            )
          }
          text={<>Tạo cặp khóa mới cho <b>{peer.name}</b>? Cấu hình hiện tại sẽ ngừng hoạt động ngay khi bạn xác nhận.</>}
        />
      )}
    </>
  );
}
