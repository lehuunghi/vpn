import { useEffect, useState, type FormEvent } from "react";
import { api, type SessionInfo } from "../api";
import { ago, dateTime } from "../format";
import { errorMessage, useAuth, useNow, useToast } from "../state";
import { Field, Modal, copyText } from "../components/ui";
import { ThemeSwitch } from "../components/ThemeSwitch";
import { roleLabel } from "../locale";

export function Account() {
  const { me, refresh } = useAuth();
  const toast = useToast();
  const now = useNow();
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [pwError, setPwError] = useState("");
  const [busy, setBusy] = useState(false);
  const [totp, setTotp] = useState<{ secret: string; uri: string } | null>(null);
  const [code, setCode] = useState("");
  const [totpError, setTotpError] = useState("");
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const [disablePw, setDisablePw] = useState("");
  const [disabling, setDisabling] = useState(false);

  const loadSessions = () => api.sessions().then(setSessions).catch(() => {});
  useEffect(() => {
    void loadSessions();
  }, []);

  async function changePassword(e: FormEvent) {
    e.preventDefault();
    setPwError("");
    if (next !== confirm) {
      setPwError("Mật khẩu mới và mật khẩu xác nhận không khớp.");
      return;
    }
    setBusy(true);
    try {
      await api.changePassword(current, next);
      setCurrent("");
      setNext("");
      setConfirm("");
      toast("Đã đổi mật khẩu và đăng xuất các phiên khác");
      void loadSessions();
    } catch (err) {
      setPwError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function startTotp() {
    try {
      setTotp(await api.totpSetup());
      setCode("");
      setTotpError("");
    } catch (err) {
      toast(errorMessage(err), "bad");
    }
  }

  async function confirmTotp(e: FormEvent) {
    e.preventDefault();
    setTotpError("");
    try {
      const r = await api.totpConfirm(code);
      setTotp(null);
      setRecovery(r.recoveryCodes);
      await refresh();
    } catch (err) {
      setTotpError(errorMessage(err));
    }
  }

  async function disableTotp(e: FormEvent) {
    e.preventDefault();
    try {
      await api.totpDisable(disablePw);
      setDisabling(false);
      setDisablePw("");
      toast("Đã tắt xác thực hai bước");
      await refresh();
    } catch (err) {
      toast(errorMessage(err), "bad");
    }
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Tài khoản</h1>
          <p>
            Đang đăng nhập bằng <b>{me?.username}</b> ({roleLabel(me?.role)}).
          </p>
        </div>
      </div>
      <div className="grid grid-2">
        <form className="card" onSubmit={changePassword}>
          <div className="card-head">
            <h2>Mật khẩu</h2>
          </div>
          <div className="card-body">
            {pwError && <div className="error">{pwError}</div>}
            <Field label="Mật khẩu hiện tại">
              <input className="input" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
            </Field>
            <Field label="Mật khẩu mới" hint="Tối thiểu 12 ký tự.">
              <input className="input" type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={12} autoComplete="new-password" />
            </Field>
            <Field label="Xác nhận mật khẩu mới">
              <input className="input" type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required autoComplete="new-password" />
            </Field>
            <button className="btn primary" type="submit" disabled={busy}>
              Đổi mật khẩu
            </button>
          </div>
        </form>
        <div className="card">
          <div className="card-head">
            <h2>Xác thực hai bước</h2>
            {me?.totpEnabled ? <span className="badge ok">bật</span> : <span className="badge">tắt</span>}
          </div>
          <div className="card-body">
            {me?.totpEnabled ? (
              <>
                <p>Mỗi lần đăng nhập đều cần mã từ ứng dụng xác thực.</p>
                <p className="muted small">
                  Còn {me.recoveryCodesLeft} mã khôi phục.
                </p>
                {!disabling ? (
                  <button className="btn" onClick={() => setDisabling(true)}>
                    Tắt
                  </button>
                ) : (
                  <form onSubmit={disableTotp}>
                    <Field label="Xác nhận bằng mật khẩu">
                      <input className="input" type="password" value={disablePw} onChange={(e) => setDisablePw(e.target.value)} required autoComplete="current-password" autoFocus />
                    </Field>
                    <div className="btn-row">
                      <button className="btn danger" type="submit">
                        Tắt xác thực hai bước
                      </button>
                      <button className="btn" type="button" onClick={() => setDisabling(false)}>
                        Hủy
                      </button>
                    </div>
                  </form>
                )}
              </>
            ) : (
              <>
                <p>Thêm mã dùng một lần theo thời gian từ ứng dụng xác thực như Aegis, Google Authenticator hoặc 1Password.</p>
                <button className="btn primary" onClick={startTotp}>
                  Thiết lập
                </button>
              </>
            )}
          </div>
        </div>
      </div>
      <div className="card mt">
        <div className="card-head">
          <h2>Giao diện</h2>
        </div>
        <div className="card-body">
          <p className="muted small" style={{ marginBottom: 10 }}>
            Giao diện tối là mặc định. Lựa chọn chỉ được lưu trong trình duyệt này.
          </p>
          <ThemeSwitch />
        </div>
      </div>
      <div className="card mt">
        <div className="card-head">
          <h2>Phiên đăng nhập</h2>
          {sessions.length > 1 && (
            <button
              className="btn sm"
              onClick={() =>
                api
                  .revokeSessions()
                  .then(() => {
                    toast("Đã đăng xuất các phiên khác");
                    void loadSessions();
                  })
                  .catch((e) => toast(errorMessage(e), "bad"))
              }
            >
              Đăng xuất các phiên khác
            </button>
          )}
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th></th>
                <th>Thời điểm đăng nhập</th>
                <th>Hoạt động gần nhất</th>
                <th>Địa chỉ IP</th>
                <th>Trình duyệt</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s, i) => (
                <tr key={i}>
                  <td>{s.current && <span className="badge accent">phiên hiện tại</span>}</td>
                  <td className="nowrap">{dateTime(s.createdAt)}</td>
                  <td className="nowrap">{ago(s.lastSeenAt, now)}</td>
                  <td className="mono">{s.ip}</td>
                  <td className="muted small" style={{ maxWidth: 360, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {s.userAgent}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {totp && (
        <Modal title="Thiết lập xác thực hai bước" onClose={() => setTotp(null)}>
          <form onSubmit={confirmTotp}>
            <div className="qr">
              <img src="/api/auth/totp/qr.png" alt="Mã QR cho ứng dụng xác thực" width={256} height={256} style={{ maxWidth: 256 }} />
              <p className="small muted">
                Không quét được? Nhập khóa này thủ công: <code>{totp.secret}</code>{" "}
                <button type="button" className="btn sm ghost" onClick={() => copyText(totp.secret).then((ok) => toast(ok ? "Đã sao chép" : "Không thể sao chép", ok ? "ok" : "bad"))}>
                  sao chép
                </button>
              </p>
            </div>
            {totpError && <div className="error">{totpError}</div>}
            <Field label="Nhập mã gồm 6 chữ số trong ứng dụng">
              <input className="input" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required autoFocus />
            </Field>
            <button className="btn primary" type="submit">
              Bật
            </button>
          </form>
        </Modal>
      )}
      {recovery && (
        <Modal title="Mã khôi phục" onClose={() => setRecovery(null)}>
          <p>Mỗi mã giúp bạn đăng nhập một lần khi mất ứng dụng xác thực. Hãy lưu ở nơi an toàn vì các mã sẽ không được hiển thị lại.</p>
          <ul className="recovery">
            {recovery.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          <div className="btn-row mt">
            <button className="btn" onClick={() => copyText(recovery.join("\n")).then((ok) => toast(ok ? "Đã sao chép" : "Không thể sao chép", ok ? "ok" : "bad"))}>
              Sao chép tất cả
            </button>
            <button className="btn primary" onClick={() => setRecovery(null)}>
              Tôi đã lưu các mã
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
