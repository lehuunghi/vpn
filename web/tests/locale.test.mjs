import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { stripTypeScriptTypes } from "node:module";
import test from "node:test";

// Thực thi trực tiếp các helper TypeScript; không cần thư viện frontend.
const localeSource = readFileSync(new URL("../src/locale.ts", import.meta.url), "utf8");
const localeURL = "data:text/javascript;base64," + Buffer.from(stripTypeScriptTypes(localeSource)).toString("base64");
const locale = await import(localeURL);
const formatSource = readFileSync(new URL("../src/format.ts", import.meta.url), "utf8")
  .replace('"./locale"', JSON.stringify(localeURL));
const format = await import("data:text/javascript;base64," + Buffer.from(stripTypeScriptTypes(formatSource)).toString("base64"));

test("Lỗi lồng nhau giữ ngữ cảnh và giá trị nhập của người dùng", () => {
  assert.equal(
    locale.localizeError('client routes: "10.0.0.1" is not a CIDR'),
    'Định tuyến thiết bị: "10.0.0.1" không phải mạng CIDR hợp lệ.',
  );
  assert.equal(
    locale.localizeError("IPv4: 10.9.0.2 is outside 10.8.0.0/24"),
    "IPv4: Địa chỉ 10.9.0.2 nằm ngoài mạng 10.8.0.0/24.",
  );
  assert.equal(locale.localizeError("password must be at least 12 characters"), "Mật khẩu phải có ít nhất 12 ký tự.");
});

test("Nhiều lỗi xác thực không bị mất và lỗi chưa biết có thông báo tiếng Việt", () => {
  const errors = locale.localizeError("MTU must be between 1280 and 9000\nendpoint port must be 1-65535");
  assert.equal(errors, "MTU phải trong khoảng 1.280–9.000.\nCổng kết nối phải trong khoảng 1–65.535.");
  assert.equal(locale.localizeError("proxy upstream unavailable", 503), "Máy chủ gặp lỗi. Vui lòng thử lại sau.");
  assert.equal(locale.localizeError("Tên thiết bị không hợp lệ."), "Tên thiết bị không hợp lệ.");
});

test("Số liệu dùng dấu phân cách Việt Nam và vẫn giữ đơn vị mạng", () => {
  assert.equal(locale.number(1234.5, 2), "1.234,50");
  assert.equal(format.bytes(1500), "1,50 KB");
  assert.equal(format.rate(125000), "1,00 Mbit/s");
  assert.equal(format.bytes(-1), "0 B");
});

test("Thời gian tương đối và mốc thời gian trống hiển thị đúng tiếng Việt", () => {
  const now = Date.parse("2026-10-06T03:00:00Z");
  assert.equal(format.ago("2026-10-06T02:58:00Z", now), "2 phút trước");
  assert.equal(format.duration("2026-10-06T01:30:00Z", now), "1 giờ 30 phút");
  assert.equal(format.ago("0001-01-01T00:00:00Z", now), "chưa từng");
  assert.equal(format.dateTime("0001-01-01T00:00:00Z"), "");
});

test("Nhật ký cũ được Việt hóa mà vẫn giữ ID và dữ liệu thiết bị", () => {
  assert.equal(locale.auditDetail("role viewer, password reset, two-factor reset"),
    "Vai trò người xem, Đã đặt lại mật khẩu, Đã đặt lại xác thực hai bước");
  assert.equal(locale.auditDetail("abc-123 10.8.0.2"), "abc-123 10.8.0.2");
  assert.equal(locale.auditAction("peer.keys_rotated"), "Thay khóa thiết bị");
  assert.equal(locale.roleLabel("viewer"), "Người xem");
});
