import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useRoute } from "wouter";
import { Plus, Search } from "lucide-react";
import { api, type Peer, type Settings } from "../api";
import { ago, bytes, rate } from "../format";
import { errorMessage, useAuth, useLive, useNow, useToast } from "../state";
import { Sparkline } from "../components/charts";
import { Segmented } from "../components/ui";
import { PeerForm } from "../components/PeerForm";
import { PeerDetail } from "../components/PeerDetail";

type Filter = "all" | "connected" | "offline" | "disabled";

export function Peers() {
  const { me } = useAuth();
  const { snapshot, peersVersion } = useLive();
  const toast = useToast();
  const now = useNow();
  const [, navigate] = useLocation();
  const [, params] = useRoute("/peers/:id");
  const [peers, setPeers] = useState<Peer[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<(Peer & { config: string }) | null>(null);
  const isAdmin = me?.role === "admin";

  // Recent throughput per peer for the sparklines: one sample per snapshot.
  const history = useRef<Map<string, number[]>>(new Map());
  useEffect(() => {
    if (!snapshot) return;
    for (const [id, l] of Object.entries(snapshot.peers)) {
      const h = history.current.get(id) ?? [];
      h.push(l.rxRate + l.txRate);
      if (h.length > 40) h.shift();
      history.current.set(id, h);
    }
  }, [snapshot]);

  const load = () =>
    Promise.all([api.peers(), api.settings()])
      .then(([p, s]) => {
        setPeers(p);
        setSettings(s);
      })
      .catch((e) => toast(errorMessage(e), "bad"));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [peersVersion]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return peers
      .map((p) => ({ p, l: snapshot?.peers[p.id] ?? p.live }))
      .filter(({ p, l }) => {
        if (q && !(p.name.toLowerCase().includes(q) || p.ipv4.includes(q) || (p.ipv6 ?? "").includes(q) || p.publicKey.toLowerCase().startsWith(q) || (l.endpoint ?? "").includes(q) || p.notes.toLowerCase().includes(q))) return false;
        switch (filter) {
          case "connected":
            return l.connected;
          case "offline":
            return !l.connected && p.enabled && !p.expired;
          case "disabled":
            return !p.enabled || p.expired;
          default:
            return true;
        }
      })
      .sort((a, b) => {
        // Connected first, then by name.
        if (a.l.connected !== b.l.connected) return a.l.connected ? -1 : 1;
        return a.p.name.localeCompare(b.p.name, "vi-VN");
      });
  }, [peers, snapshot, query, filter]);

  const selected = params?.id ? peers.find((p) => p.id === params.id) : undefined;
  const counts = useMemo(() => {
    let connected = 0;
    let disabled = 0;
    for (const p of peers) {
      const l = snapshot?.peers[p.id] ?? p.live;
      if (l.connected) connected++;
      if (!p.enabled || p.expired) disabled++;
    }
    return { connected, disabled, offline: peers.length - connected - disabled };
  }, [peers, snapshot]);

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Thiết bị</h1>
          <p>
            {peers.length} thiết bị · {counts.connected} đang kết nối
          </p>
        </div>
        <div className="toolbar">
          <div className="search-wrap" style={{ position: "relative" }}>
            <Search size={14} style={{ position: "absolute", left: 9, top: 10, color: "var(--fg-faint)" }} />
            <input className="input search" style={{ paddingLeft: 28 }} placeholder="Tìm tên, địa chỉ, khóa…" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <Segmented
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "Tất cả" },
              { value: "connected", label: `Đang kết nối ${counts.connected}` },
              { value: "offline", label: `Chưa kết nối ${counts.offline}` },
              { value: "disabled", label: `Vô hiệu hóa ${counts.disabled}` },
            ]}
          />
          {isAdmin && (
            <button className="btn primary" onClick={() => setCreating(true)}>
              <Plus /> Thêm thiết bị
            </button>
          )}
        </div>
      </div>

      <div className="card">
        {rows.length === 0 ? (
          <div className="empty">{peers.length === 0 ? "Chưa có thiết bị. Hãy thêm thiết bị và quét mã QR bằng ứng dụng WireGuard." : "Không có kết quả phù hợp."}</div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Thiết bị</th>
                  <th>Địa chỉ</th>
                  <th className="hide-md">Điểm kết nối</th>
                  <th>Bắt tay</th>
                  <th className="right hide-sm">Tốc độ</th>
                  <th className="hide-md"></th>
                  <th className="right">Dữ liệu truyền</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(({ p, l }) => {
                  const state = !p.enabled ? "disabled" : p.expired ? "expired" : l.connected ? "on" : "off";
                  return (
                    <tr key={p.id} className="clickable" onClick={() => navigate(`/peers/${p.id}`)}>
                      <td>
                        <span className={`dot ${state}`} title={{ disabled: "Đã vô hiệu hóa", expired: "Đã hết hạn", on: "Đang kết nối", off: "Chưa kết nối" }[state]} />
                        {p.name}
                        {!p.enabled && <span className="badge bad" style={{ marginLeft: 8 }}>đã vô hiệu hóa</span>}
                        {p.enabled && p.expired && <span className="badge warn" style={{ marginLeft: 8 }}>đã hết hạn</span>}
                        {!p.serverKeys && <span className="badge" style={{ marginLeft: 8 }} title="Thiết bị tự giữ khóa riêng">khóa thiết bị</span>}
                      </td>
                      <td className="mono nowrap">{p.ipv4}</td>
                      <td className="mono nowrap hide-md">{l.endpoint || <span className="faint">—</span>}</td>
                      <td className="nowrap">{ago(l.lastHandshake, now)}</td>
                      <td className="num right hide-sm">{l.connected ? `↓ ${rate(l.rxRate)} ↑ ${rate(l.txRate)}` : <span className="faint">—</span>}</td>
                      <td className="hide-md">{l.connected && <Sparkline values={history.current.get(p.id) ?? []} />}</td>
                      <td className="num right">
                        {bytes(l.rx)} <span className="faint">/</span> {bytes(l.tx)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {creating && settings && (
        <PeerForm
          settings={settings}
          onClose={() => setCreating(false)}
          onSaved={(p) => {
            setCreating(false);
            toast(`Đã tạo thiết bị ${p.name}`);
            void load().then(() => {
              setCreated(p as Peer & { config: string });
              navigate(`/peers/${p.id}`);
            });
          }}
        />
      )}
      {selected && settings && (
        <PeerDetail
          key={selected.id + selected.updatedAt}
          peer={selected}
          live={snapshot?.peers[selected.id] ?? selected.live}
          settings={settings}
          isAdmin={!!isAdmin}
          initialTab={created?.id === selected.id ? "config" : "overview"}
          initialConfig={created?.id === selected.id ? created.config : undefined}
          onClose={() => {
            setCreated(null);
            navigate("/peers");
          }}
          onChanged={() => void load()}
        />
      )}
    </>
  );
}
