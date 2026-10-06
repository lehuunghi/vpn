import { useEffect, useState } from "react";
import { Link } from "wouter";
import { AlertTriangle, Copy } from "lucide-react";
import { api, type Peer, type Range, type Status, type TrafficPoint } from "../api";
import { ago, bytes, duration, rate } from "../format";
import { errorMessage, useLive, useNow, useToast } from "../state";
import { Legend, TrafficChart } from "../components/charts";
import { Segmented, copyText } from "../components/ui";
import { backendLabel, technicalMessage } from "../locale";

const rangeMs: Record<Range, number> = { "1h": 3600e3, "24h": 86400e3, "7d": 7 * 86400e3, "30d": 30 * 86400e3 };

export function Dashboard() {
  const { snapshot, peersVersion } = useLive();
  const toast = useToast();
  const now = useNow();
  const [status, setStatus] = useState<Status | null>(null);
  const [peers, setPeers] = useState<Peer[]>([]);
  const [range, setRange] = useState<Range>("24h");
  const [series, setSeries] = useState<TrafficPoint[]>([]);
  const [showSysctls, setShowSysctls] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    api.status().then(setStatus).catch((e) => setError(errorMessage(e)));
  }, []);
  useEffect(() => {
    api.peers().then(setPeers).catch((e) => setError(errorMessage(e)));
  }, [peersVersion]);
  useEffect(() => {
    let live = true;
    const load = () => api.usage(range).then((s) => live && setSeries(s)).catch(() => {});
    void load();
    const t = setInterval(load, 60_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [range]);

  const totals = snapshot?.totals ?? status?.totals;
  const connected = peers
    .map((p) => ({ p, l: snapshot?.peers[p.id] ?? p.live }))
    .filter((x) => x.l.connected)
    .sort((a, b) => b.l.rxRate + b.l.txRate - (a.l.rxRate + a.l.txRate));
  const unapplied = status?.sysctls.filter((s) => !s.applied) ?? [];

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Bảng điều khiển</h1>
          <p>{status ? `${status.interface} qua UDP ${status.listenPort} · ${backendLabel(status.backend)}` : " "}</p>
        </div>
      </div>
      {error && <div className="error">{error}</div>}
      {status?.backend === "userspace" && (
        <div className="notice">
          <AlertTriangle size={14} style={{ verticalAlign: -2 }} /> Đang dùng chế độ xử lý trong không gian người dùng (wireguard-go). Nạp mô-đun <code>wireguard</code> vào nhân máy chủ để tăng tốc độ truyền lên nhiều lần.
        </div>
      )}
      {status?.backend === "mock" && <div className="notice">Chế độ mô phỏng: không có VPN thực. Lưu lượng và quá trình bắt tay được mô phỏng.</div>}
      {status?.firewallError && (
        <div className="error">
          <AlertTriangle size={14} style={{ verticalAlign: -2 }} /> Không áp dụng được quy tắc tường lửa: {technicalMessage(status.firewallError)}. Thiết bị có thể kết nối nhưng không truy cập được mạng bên ngoài máy chủ.
        </div>
      )}
      {unapplied.some((s) => s.required) && (
        <div className="error">
          <AlertTriangle size={14} style={{ verticalAlign: -2 }} /> Chuyển tiếp IP đang tắt và không thể bật. Thêm <code>net.ipv4.ip_forward=1</code> vào cấu hình sysctl của container.
        </div>
      )}

      <div className="grid grid-4">
        <Stat label="Đang kết nối" value={`${totals?.connected ?? 0}`} sub={`trong ${totals?.active ?? 0} thiết bị đã bật · ${totals?.peers ?? 0} tổng cộng`} />
        <Stat label="Tốc độ truyền" value={rate((totals?.rxRate ?? 0) + (totals?.txRate ?? 0))} sub={`↓ ${rate(totals?.rxRate ?? 0)} · ↑ ${rate(totals?.txRate ?? 0)}`} />
        <Stat label="Đã nhận" value={bytes(totals?.rx ?? 0)} sub="từ thiết bị, từ trước đến nay" />
        <Stat label="Đã gửi" value={bytes(totals?.tx ?? 0)} sub="đến thiết bị, từ trước đến nay" />
      </div>

      <div className="grid grid-main mt">
        <div className="card">
          <div className="card-head">
            <h2>Lưu lượng</h2>
            <div className="toolbar">
              <Legend />
              <Segmented value={range} onChange={setRange} options={[{ value: "1h", label: "1 giờ" }, { value: "24h", label: "24 giờ" }, { value: "7d", label: "7 ngày" }, { value: "30d", label: "30 ngày" }]} />
            </div>
          </div>
          <div className="card-body">
            <TrafficChart points={series} from={now - rangeMs[range]} to={now} bucketSeconds={range === "30d" ? 3600 : 300} />
            <div className="small faint">Dữ liệu theo mỗi 5 phút{range === "30d" ? ", hiển thị theo giờ" : ""}. Tốc độ trực tiếp phía trên được cập nhật vài giây một lần.</div>
          </div>
        </div>
        <div className="card">
          <div className="card-head">
            <h2>Máy chủ</h2>
          </div>
          <div className="card-body">
            {status && (
              <dl className="kv">
                <dt>Khóa công khai</dt>
                <dd className="mono">
                  {status.publicKey}{" "}
                  <button className="btn icon ghost sm" title="Sao chép" onClick={() => copyText(status.publicKey).then((ok) => toast(ok ? "Đã sao chép" : "Không thể sao chép", ok ? "ok" : "bad"))}>
                    <Copy />
                  </button>
                </dd>
                <dt>Điểm kết nối</dt>
                <dd className="mono">
                  {status.settings.endpointHost}:{status.settings.endpointPort}
                </dd>
                <dt>VPN</dt>
                <dd className="mono">{status.addresses.join(", ")}</dd>
                <dt>MTU</dt>
                <dd>{status.settings.mtu}</dd>
                <dt>Giao diện ra ngoài</dt>
                <dd>{status.egress || (status.firewallManaged ? "bất kỳ" : "không được quản lý")}</dd>
                <dt>Tường lửa</dt>
                <dd>{status.firewallManaged ? (status.firewallError ? <span className="badge bad">thất bại</span> : <span className="badge ok">nftables</span>) : <span className="badge">do máy chủ quản lý</span>}</dd>
                <dt>Thời gian hoạt động</dt>
                <dd>{duration(status.startedAt, now) || "—"}</dd>
                <dt>Phiên bản</dt>
                <dd>{status.version}</dd>
              </dl>
            )}
            {status && status.sysctls.length > 0 && (
              <div className="mt small">
                <button className="btn sm ghost" onClick={() => setShowSysctls((v) => !v)} style={{ marginLeft: -8 }}>
                  {showSysctls ? "Ẩn" : "Hiện"} tối ưu nhân hệ thống ({status.sysctls.length - unapplied.length}/{status.sysctls.length} đã áp dụng)
                </button>
                {showSysctls && (
                  <div className="table-wrap mt">
                    <table>
                      <thead>
                        <tr>
                          <th>sysctl</th>
                          <th>Mong muốn</th>
                          <th>Hiện tại</th>
                        </tr>
                      </thead>
                      <tbody>
                        {status.sysctls.map((s) => (
                          <tr key={s.key} title={s.error ? `${technicalMessage(s.why)}. ${technicalMessage(s.error)}` : technicalMessage(s.why)}>
                            <td className="mono">{s.key}</td>
                            <td className="mono">{s.wanted}</td>
                            <td className="mono">
                              {s.current || "?"} {s.applied ? <span className="badge ok">đã áp dụng</span> : <span className={`badge ${s.required ? "bad" : "warn"}`}>chưa áp dụng</span>}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {unapplied.length > 0 && <p className="faint mt">Giá trị “chưa áp dụng” là tham số sysctl toàn hệ thống mà container không thể thay đổi. Hãy áp dụng trên máy chủ; xem docs/performance.md.</p>}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="card mt">
        <div className="card-head">
          <h2>Đang kết nối</h2>
          <Link href="/peers" className="small">
            Tất cả thiết bị →
          </Link>
        </div>
        {connected.length === 0 ? (
          <div className="empty">Không có thiết bị bắt tay trong {status?.settings.connectedWindow ?? 180} giây gần nhất.</div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Thiết bị</th>
                  <th>Địa chỉ</th>
                  <th className="hide-md">Điểm kết nối</th>
                  <th className="hide-sm">Phiên kết nối</th>
                  <th>Bắt tay</th>
                  <th className="right">Tốc độ</th>
                  <th className="right">Dữ liệu truyền</th>
                </tr>
              </thead>
              <tbody>
                {connected.map(({ p, l }) => (
                  <tr key={p.id} className="clickable" onClick={() => (window.location.hash = "")}>
                    <td>
                      <Link href={`/peers/${p.id}`}>
                        <span className="dot on" />
                        {p.name}
                      </Link>
                    </td>
                    <td className="mono">{p.ipv4}</td>
                    <td className="mono hide-md">{l.endpoint ?? "—"}</td>
                    <td className="hide-sm">{duration(l.connectedSince, now)}</td>
                    <td>{ago(l.lastHandshake, now)}</td>
                    <td className="num right">
                      ↓ {rate(l.rxRate)} · ↑ {rate(l.txRate)}
                    </td>
                    <td className="num right">
                      {bytes(l.rx)} / {bytes(l.tx)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </>
  );
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="card stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  );
}
