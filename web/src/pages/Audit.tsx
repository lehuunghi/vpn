import { useEffect, useState } from "react";
import { api, type AuditEntry } from "../api";
import { dateTime } from "../format";
import { errorMessage, useToast } from "../state";
import { auditAction, auditDetail } from "../locale";

export function Audit() {
  const toast = useToast();
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [query, setQuery] = useState("");

  useEffect(() => {
    api.audit(500).then(setEntries).catch((e) => toast(errorMessage(e), "bad"));
  }, [toast]);

  const q = query.trim().toLowerCase();
  const rows = q ? entries.filter((e) => [e.actor, e.action, auditAction(e.action), e.target, e.detail, auditDetail(e.detail), e.ip].some((v) => v.toLowerCase().includes(q))) : entries;

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Nhật ký hoạt động</h1>
          <p>Ghi lại mọi thao tác quản trị, mới nhất trước. Hệ thống lưu 5.000 mục gần nhất.</p>
        </div>
        <input className="input search" placeholder="Lọc…" value={query} onChange={(e) => setQuery(e.target.value)} />
      </div>
      <div className="card">
        {rows.length === 0 ? (
          <div className="empty">Chưa có hoạt động được ghi lại.</div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Thời gian</th>
                  <th>Người thực hiện</th>
                  <th>Thao tác</th>
                  <th>Đối tượng</th>
                  <th>Chi tiết</th>
                  <th>Địa chỉ IP</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((e) => (
                  <tr key={e.id}>
                    <td className="nowrap">{dateTime(e.at)}</td>
                    <td>{e.actor}</td>
                    <td>
                      <span className={`badge ${e.action.includes("failed") ? "bad" : e.action.includes("deleted") || e.action.includes("disabled") ? "warn" : ""}`}>{auditAction(e.action)}</span>
                    </td>
                    <td>{e.target}</td>
                    <td className="muted">{auditDetail(e.detail)}</td>
                    <td className="mono">{e.ip}</td>
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
