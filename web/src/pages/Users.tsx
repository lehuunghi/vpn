import { useEffect, useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import { api, type User } from "../api";
import { ago, dateTime } from "../format";
import { errorMessage, useAuth, useNow, useToast } from "../state";
import { Confirm, Field, Modal } from "../components/ui";
import { roleLabel } from "../locale";

export function UsersPage() {
  const { me } = useAuth();
  const toast = useToast();
  const now = useNow();
  const [users, setUsers] = useState<User[]>([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);
  const [busy, setBusy] = useState(false);
  const isAdmin = me?.role === "admin";

  const load = () => api.users().then(setUsers).catch((e) => toast(errorMessage(e), "bad"));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Người dùng</h1>
          <p>Quản trị viên có toàn quyền quản lý; người xem chỉ có thể xem thông tin.</p>
        </div>
        {isAdmin && (
          <button className="btn primary" onClick={() => setCreating(true)}>
            <Plus /> Thêm người dùng
          </button>
        )}
      </div>
      <div className="card">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Tên đăng nhập</th>
                <th>Vai trò</th>
                <th>Xác thực hai bước</th>
                <th>Đăng nhập gần nhất</th>
                <th>Ngày tạo</th>
                {isAdmin && <th></th>}
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>
                    {u.username} {u.id === me?.id && <span className="badge accent">bạn</span>}
                  </td>
                  <td>
                    <span className={`badge ${u.role === "admin" ? "accent" : ""}`}>{roleLabel(u.role)}</span>
                  </td>
                  <td>{u.totpEnabled ? <span className="badge ok">bật</span> : <span className="badge">tắt</span>}</td>
                  <td>{ago(u.lastLoginAt, now)}</td>
                  <td>{dateTime(u.createdAt)}</td>
                  {isAdmin && (
                    <td className="actions">
                      <button className="btn sm" onClick={() => setEditing(u)}>
                        Chỉnh sửa
                      </button>{" "}
                      {u.id !== me?.id && (
                        <button className="btn sm danger" onClick={() => setDeleting(u)}>
                          Xóa
                        </button>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {creating && (
        <UserForm
          onClose={() => setCreating(false)}
          onSaved={() => {
            setCreating(false);
            toast("Đã tạo người dùng");
            void load();
          }}
        />
      )}
      {editing && (
        <UserEdit
          user={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            toast("Đã cập nhật người dùng");
            void load();
          }}
        />
      )}
      {deleting && (
        <Confirm
          title="Xóa người dùng"
          danger
          confirmLabel="Xóa"
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            setBusy(true);
            try {
              await api.deleteUser(deleting.id);
              toast("Đã xóa người dùng");
              setDeleting(null);
              void load();
            } catch (e) {
              toast(errorMessage(e), "bad");
            } finally {
              setBusy(false);
            }
          }}
          text={
            <>
              Xóa <b>{deleting.username}</b>? Các phiên đăng nhập của người dùng sẽ kết thúc ngay.
            </>
          }
        />
      )}
    </>
  );
}

function UserForm({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("admin");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createUser({ username, password, role });
      onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title="Thêm người dùng"
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            Hủy
          </button>
          <button className="btn primary" type="submit" form="user-form" disabled={busy}>
            Tạo
          </button>
        </>
      }
    >
      <form id="user-form" onSubmit={submit}>
        {error && <div className="error">{error}</div>}
        <Field label="Tên đăng nhập">
          <input className="input" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} required autoComplete="off" />
        </Field>
        <Field label="Mật khẩu" hint="Tối thiểu 12 ký tự. Hãy yêu cầu người dùng đổi mật khẩu sau khi đăng nhập.">
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={12} autoComplete="new-password" />
        </Field>
        <Field label="Vai trò">
          <select className="input" value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="admin">Quản trị viên</option>
            <option value="viewer">Người xem (chỉ đọc)</option>
          </select>
        </Field>
      </form>
    </Modal>
  );
}

function UserEdit({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const { me } = useAuth();
  const [role, setRole] = useState(user.role);
  const [password, setPassword] = useState("");
  const [resetTotp, setResetTotp] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.updateUser(user.id, { role: role !== user.role ? role : undefined, password: password || undefined, resetTotp });
      onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={`Chỉnh sửa ${user.username}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            Hủy
          </button>
          <button className="btn primary" type="submit" form="user-edit" disabled={busy}>
            Lưu
          </button>
        </>
      }
    >
      <form id="user-edit" onSubmit={submit}>
        {error && <div className="error">{error}</div>}
        <Field label="Vai trò" hint={user.id === me?.id ? "Bạn không thể đổi vai trò của chính mình." : undefined}>
          <select className="input" value={role} onChange={(e) => setRole(e.target.value as User["role"])} disabled={user.id === me?.id}>
            <option value="admin">Quản trị viên</option>
            <option value="viewer">Người xem (chỉ đọc)</option>
          </select>
        </Field>
        <Field label="Mật khẩu mới" hint="Để trống để giữ mật khẩu hiện tại. Đặt mật khẩu mới sẽ đăng xuất tất cả phiên của người dùng.">
          <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={12} autoComplete="new-password" />
        </Field>
        {user.totpEnabled && (
          <div className="check">
            <input id="reset-totp" type="checkbox" checked={resetTotp} onChange={(e) => setResetTotp(e.target.checked)} />
            <label htmlFor="reset-totp">
              Đặt lại xác thực hai bước
              <span className="hint">Dùng khi mất ứng dụng xác thực. Người dùng có thể thiết lập lại trong trang tài khoản.</span>
            </label>
          </div>
        )}
      </form>
    </Modal>
  );
}
