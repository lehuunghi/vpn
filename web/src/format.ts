import { DEFAULT_LOCALE, number } from "./locale";

const units = ["B", "KB", "MB", "GB", "TB", "PB"];

export function bytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  let i = 0;
  let v = n;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  const digits = i === 0 ? 0 : v < 10 ? 2 : v < 100 ? 1 : 0;
  return `${number(v, digits)} ${units[i]}`;
}

export function rate(bytesPerSecond: number): string {
  const bits = bytesPerSecond * 8;
  if (bits < 1000) return `${number(Math.round(bits))} bit/s`;
  if (bits < 1e6) return `${number(bits / 1e3, bits < 1e4 ? 1 : 0)} kbit/s`;
  if (bits < 1e9) return `${number(bits / 1e6, bits < 1e7 ? 2 : 1)} Mbit/s`;
  return `${number(bits / 1e9, 2)} Gbit/s`;
}

export function isZeroTime(s?: string): boolean {
  return !s || s.startsWith("0001-01-01");
}

export function ago(s?: string, now = Date.now()): string {
  if (isZeroTime(s)) return "chưa từng";
  const t = new Date(s!).getTime();
  const d = Math.max(0, Math.round((now - t) / 1000));
  if (d < 5) return "vừa xong";
  if (d < 60) return `${d} giây trước`;
  if (d < 3600) return `${Math.floor(d / 60)} phút trước`;
  if (d < 86400) return `${Math.floor(d / 3600)} giờ ${Math.floor((d % 3600) / 60)} phút trước`;
  return `${Math.floor(d / 86400)} ngày trước`;
}

export function duration(from?: string, now = Date.now()): string {
  if (isZeroTime(from)) return "";
  const d = Math.max(0, Math.round((now - new Date(from!).getTime()) / 1000));
  const h = Math.floor(d / 3600);
  const m = Math.floor((d % 3600) / 60);
  const s = d % 60;
  if (h > 0) return `${h} giờ ${m} phút`;
  if (m > 0) return `${m} phút ${s} giây`;
  return `${s} giây`;
}

export function dateTime(s?: string): string {
  if (isZeroTime(s)) return "";
  return new Date(s!).toLocaleString(DEFAULT_LOCALE, { dateStyle: "medium", timeStyle: "short" });
}

export function shortKey(k: string): string {
  return k.length > 12 ? `${k.slice(0, 8)}…${k.slice(-4)}` : k;
}

// Renders a Date as the value an <input type="datetime-local"> wants.
export function toLocalInput(s?: string): string {
  if (isZeroTime(s)) return "";
  const d = new Date(s!);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromLocalInput(v: string): string | null {
  if (!v) return null;
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}
