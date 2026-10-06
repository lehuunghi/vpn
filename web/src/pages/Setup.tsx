import { useState, type FormEvent } from "react";
import { api } from "../api";
import { errorMessage, useAuth } from "../state";
import { Field } from "../components/ui";
import { Mark } from "../components/Mark";
import { Legal } from "../components/Legal";

export function Setup() {
  const { refresh } = useAuth();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [endpointHost, setEndpointHost] = useState(window.location.hostname === "localhost" ? "" : window.location.hostname);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (password !== confirm) {
      setError("Mật khẩu và mật khẩu xác nhận không khớp.");
      return;
    }
    setBusy(true);
    try {
      await api.setup({ username, password, endpointHost });
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth">
      <form className="card" onSubmit={submit}>
        <div className="card-body">
          <div className="brand">
            <div className="brand-mark">
              <Mark size={44} />
            </div>
            <div className="brand-name">VPN20</div>
          </div>
          <h1>Chào mừng</h1>
          <p className="muted" style={{ textAlign: "center", marginBottom: 16 }}>
            Tạo tài khoản quản trị đầu tiên. Biểu mẫu này chỉ dùng một lần.
          </p>
          {error && <div className="error">{error}</div>}
          <Field label="Tên đăng nhập">
            <input className="input" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Mật khẩu" hint="Tối thiểu 12 ký tự. Ưu tiên mật khẩu dài.">
            <input className="input" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={12} />
          </Field>
          <Field label="Xác nhận mật khẩu">
            <input className="input" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
          </Field>
          <Field label="Địa chỉ kết nối công khai" hint="Tên miền hoặc IP mà thiết bị sẽ kết nối đến. Có thể thay đổi sau trong Cài đặt.">
            <input className="input" value={endpointHost} onChange={(e) => setEndpointHost(e.target.value)} placeholder="vpn.example.com" required />
          </Field>
          <button className="btn primary" type="submit" disabled={busy} style={{ width: "100%", justifyContent: "center" }}>
            {busy ? "…" : "Tạo tài khoản quản trị"}
          </button>
          <Legal center />
        </div>
      </form>
    </div>
  );
}
