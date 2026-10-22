// VPN20 DDNS + DNS-01 broker. Cloudflare/admin credentials never leave this Worker.
const DOMAIN = "vpn.20.com.vn";
const API = "https://api.cloudflare.com/client/v4";
const MIN_INTERVAL_MS = 30_000;
const VERIFY_INTERVAL_MS = 15 * 60_000;
const RESERVED = new Set(["api", "ddns", "admin", "www", "mail", "ns", "ns1", "ns2", "acme", "auth"]);

class APIError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const fail = (status, code, message) => { throw new APIError(status, code, message); };
const reply = (data, status = 200) => new Response(JSON.stringify(data), {
  status,
  headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" },
});
const randomKey = () => Array.from(crypto.getRandomValues(new Uint8Array(32)), n => n.toString(16).padStart(2, "0")).join("");
const digest = async value => new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value)));
const hash = async value => Array.from(await digest(value), n => n.toString(16).padStart(2, "0")).join("");

function bearer(request) {
  const match = /^Bearer ([A-Za-z0-9_-]{32,256})$/.exec(request.headers.get("Authorization") || "");
  if (!match) fail(401, "unauthorized", "Thiếu hoặc sai khóa xác thực.");
  return match[1];
}

async function admin(request, env) {
  const [received, expected] = await Promise.all([digest(bearer(request)), digest(env.ADMIN_TOKEN)]);
  let mismatch = 0;
  for (let i = 0; i < expected.length; i++) mismatch |= received[i] ^ expected[i];
  if (mismatch) fail(401, "unauthorized", "Sai khóa quản trị.");
}

function name(value) {
  if (typeof value !== "string") fail(400, "invalid_name", "Tên phải là một chuỗi.");
  const normalized = value.toLowerCase();
  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(normalized) || RESERVED.has(normalized)) {
    fail(400, "invalid_name", "Chọn một nhãn DNS, tối đa 63 ký tự a-z, 0-9 và dấu gạch nối.");
  }
  return normalized;
}

async function body(request, allowed) {
  if (Number(request.headers.get("Content-Length")) > 4096) fail(413, "too_large", "JSON quá lớn.");
  const reader = request.body?.getReader();
  const chunks = [];
  let length = 0;
  if (reader) {
    while (true) {
      const part = await reader.read();
      if (part.done) break;
      length += part.value.byteLength;
      if (length > 4096) {
        await reader.cancel();
        fail(413, "too_large", "JSON quá lớn.");
      }
      chunks.push(part.value);
    }
  }
  if (!length) return {};
  if (!(request.headers.get("Content-Type") || "").toLowerCase().startsWith("application/json")) {
    fail(415, "json_required", "Dùng Content-Type: application/json.");
  }
  const data = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length; }
  let parsed;
  try { parsed = JSON.parse(new TextDecoder().decode(data)); } catch { fail(400, "invalid_json", "JSON không hợp lệ."); }
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object" || Object.keys(parsed).some(k => !allowed.includes(k))) {
    fail(400, "invalid_fields", "JSON chứa trường không được hỗ trợ.");
  }
  return parsed;
}

function publicIP(value) {
  if (typeof value !== "string" || value.length > 45) fail(400, "invalid_ip", "Cần địa chỉ IP public hợp lệ.");
  if (value.includes(":")) {
    if (!/^[0-9a-f:]+$/i.test(value)) fail(400, "invalid_ip", "IPv6 không hợp lệ.");
    let ip;
    try { ip = new URL(`http://[${value}]/`).hostname.slice(1, -1).toLowerCase(); }
    catch { fail(400, "invalid_ip", "IPv6 không hợp lệ."); }
    // Accept global-unicast space, excluding documentation prefixes.
    if (!/^[23][0-9a-f]{3}:/.test(ip) || ip.startsWith("2001:db8:") || ip.startsWith("3fff:")) {
      fail(400, "non_public_ip", "Dùng IPv6 global của máy chủ.");
    }
    return { ip, type: "AAAA" };
  }
  if (!/^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(value)) fail(400, "invalid_ip", "IPv4 không hợp lệ.");
  const [a, b, c, d] = value.split(".").map(Number);
  if ([a, b, c, d].some(n => n > 255)) fail(400, "invalid_ip", "IPv4 không hợp lệ.");
  if (a === 0 || a === 10 || a === 127 || a >= 224 ||
      (a === 100 && b >= 64 && b <= 127) || (a === 169 && b === 254) ||
      (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) ||
      (a === 192 && b === 0 && (c === 0 || c === 2)) ||
      (a === 198 && (b === 18 || b === 19 || (b === 51 && c === 100))) ||
      (a === 203 && b === 0 && c === 113)) {
    fail(400, "non_public_ip", "Dùng IPv4 public; không dùng IP LAN hoặc CGNAT.");
  }
  return { ip: value, type: "A" };
}

async function cloudflare(env, path, method = "GET", payload) {
  const response = await fetch(`${API}/zones/${env.CF_ZONE_ID}/dns_records${path}`, {
    method,
    headers: { Authorization: `Bearer ${env.CF_API_TOKEN}`, "Content-Type": "application/json" },
    body: payload === undefined ? undefined : JSON.stringify(payload),
    signal: AbortSignal.timeout(8000),
  });
  let data;
  try { data = await response.json(); } catch { fail(502, "dns_api_error", "Cloudflare trả dữ liệu không hợp lệ."); }
  if (!response.ok || !data.success) {
    const codes = (data.errors || []).map(e => Number(e.code)).filter(Number.isFinite).join(",");
    fail(502, "dns_api_error", `Cloudflare HTTP ${response.status}${codes ? `, mã ${codes}` : ""}. Kiểm tra token, Zone ID và hạn mức.`);
  }
  return data.result;
}

async function update(request, env) {
  const keyHash = await hash(bearer(request));
  const host = await env.DB.prepare("SELECT * FROM hosts WHERE token_hash=? AND disabled=0").bind(keyHash).first();
  if (!host) fail(401, "unauthorized", "Khóa không hợp lệ hoặc đã bị thu hồi.");
  const input = await body(request, ["ip", "ipv4", "ipv6"]);
  const paired = "ipv4" in input || "ipv6" in input;
  if (paired && "ip" in input) fail(400, "invalid_fields", "Không trộn ip với ipv4/ipv6.");
  const changes = [];
  if (paired) {
    for (const [field, type] of [["ipv4", "A"], ["ipv6", "AAAA"]]) {
      if (!(field in input)) continue;
      if (input[field] === null) changes.push({ ip: null, type });
      else {
        const parsed = publicIP(input[field]);
        if (parsed.type !== type) fail(400, "invalid_ip", "Sai loại địa chỉ IP.");
        changes.push(parsed);
      }
    }
    if (!changes.some(item => item.ip)) fail(400, "invalid_ip", "Cần giữ ít nhất một IP public.");
  } else changes.push(publicIP(input.ip === undefined ? request.headers.get("CF-Connecting-IP") : input.ip));
  const now = Date.now();
  const lock = crypto.randomUUID();
  const result = await env.DB.prepare(
    "UPDATE hosts SET lock_id=?,lock_until=?,last_attempt=? WHERE id=? AND token_hash=? AND disabled=0 AND lock_until<=? AND last_attempt<=?"
  ).bind(lock, now + 30_000, now, host.id, keyHash, now, now - MIN_INTERVAL_MS).run();
  if (!result.meta.changes) {
    const response = reply({ ok: false, error: "rate_limited", message: "Đợi ít nhất 30 giây giữa hai lần cập nhật." }, 429);
    response.headers.set("Retry-After", "30");
    return response;
  }
  try {
    const outcomes = [];
    for (const { ip, type } of changes) {
    const isV4 = type === "A";
    const savedIP = isV4 ? host.ipv4 : host.ipv6;
    const verified = isV4 ? host.verified4 : host.verified6;
    if (ip === savedIP && now - verified < VERIFY_INTERVAL_MS) {
      outcomes.push({ ip, type, changed: false });
      continue;
    }
    const marker = `VPN20-DDNS:${host.id}`;
    const query = new URLSearchParams({ name: host.hostname, type, per_page: "100" });
    const records = await cloudflare(env, `?${query}`);
    if (!Array.isArray(records) || records.length > 1 || records.some(r => r.comment !== marker)) {
      fail(409, "dns_record_conflict", "Tên này có bản ghi DNS ngoài quyền quản lý của API. Cần quản trị viên kiểm tra.");
    }
    const record = records[0];
    let changed;
    if (ip === null) {
      changed = Boolean(record);
      if (record) await cloudflare(env, "/" + record.id, "DELETE");
    } else {
      const payload = { type, name: host.hostname, content: ip, ttl: 60, proxied: false, comment: marker };
      changed = !record || record.content !== ip || record.ttl !== 60 || record.proxied;
      if (changed) await cloudflare(env, record ? "/" + record.id : "", record ? "PATCH" : "POST", payload);
    }
    const sql = isV4
      ? "UPDATE hosts SET ipv4=?,verified4=? WHERE id=? AND token_hash=? AND disabled=0"
      : "UPDATE hosts SET ipv6=?,verified6=? WHERE id=? AND token_hash=? AND disabled=0";
    await env.DB.prepare(sql).bind(ip, now, host.id, keyHash).run();
    outcomes.push({ ip, type, changed });
    }
    return reply(paired ? { ok: true, hostname: host.hostname, records: outcomes }
      : { ok: true, hostname: host.hostname, ...outcomes[0] });
  } finally {
    await env.DB.prepare("UPDATE hosts SET lock_id=NULL,lock_until=0 WHERE id=? AND lock_id=?").bind(host.id, lock).run();
  }
}

function limit(value, fallback, maximum) {
  const n = Number(value);
  return Number.isSafeInteger(n) && n > 0 && n <= maximum ? n : fallback;
}

async function ownedHost(request, env) {
  const keyHash = await hash(bearer(request));
  const host = await env.DB.prepare("SELECT * FROM hosts WHERE token_hash=? AND disabled=0").bind(keyHash).first();
  if (!host) fail(401, "unauthorized", "Khóa máy không hợp lệ hoặc đã bị thu hồi.");
  return host;
}

// Client generates and saves its random 256-bit key BEFORE calling claim.
// This makes a lost response/restart retryable without giving clients ADMIN_TOKEN.
async function claim(request, env) {
  if (env.SELF_REGISTRATION === "false") fail(403, "registration_disabled", "Dịch vụ đã tắt đăng ký tự động.");
  const key = bearer(request);
  if (!/^[a-f0-9]{64}$/.test(key)) fail(400, "invalid_key", "Khóa máy phải có 256 bit ngẫu nhiên.");
  const keyHash = await hash(key);
  const input = await body(request, ["name"]);
  const label = name(input.name);
  const hostname = label + "." + DOMAIN;
  const existing = await env.DB.prepare("SELECT * FROM hosts WHERE token_hash=?").bind(keyHash).first();
  if (existing) {
    if (existing.disabled) fail(401, "unauthorized", "Khóa máy đã bị thu hồi.");
    if (existing.label !== label) fail(409, "key_in_use", "Khóa này đã gắn với một hostname khác.");
    return reply({ ok: true, hostname });
  }
  if (await env.DB.prepare("SELECT id FROM hosts WHERE label=?").bind(label).first()) {
    fail(409, "name_taken", "Tên subdomain đã được sử dụng.");
  }
  const now = Date.now();
  // Keyed daily hash: do not retain the requesting IP in D1.
  const ip = publicIP(request.headers.get("CF-Connecting-IP")).ip;
  const bucket = await hash(env.ADMIN_TOKEN + ":" + Math.floor(now / 86400000) + ":" + ip);
  await env.DB.prepare("DELETE FROM claim_buckets WHERE expires_at<?").bind(now).run();
  const rate = await env.DB.prepare(
    "INSERT INTO claim_buckets(bucket,attempts,expires_at) VALUES(?,1,?) ON CONFLICT(bucket) DO UPDATE SET attempts=attempts+1 WHERE attempts<?"
  ).bind(bucket, now + 86400000, limit(env.CLAIMS_PER_IP_DAY, 3, 100)).run();
  if (!rate.meta.changes) fail(429, "claim_limit", "Đã đạt hạn mức đăng ký của mạng này. Thử lại ngày mai.");
  const existingDNS = await cloudflare(env, "?" + new URLSearchParams({ name: hostname, per_page: "100" }));
  if (!Array.isArray(existingDNS) || existingDNS.length) {
    fail(409, "dns_record_conflict", "Tên đã có bản ghi DNS ngoài quyền quản lý.");
  }
  const result = await env.DB.prepare(
    "INSERT INTO hosts(id,label,hostname,token_hash,created_at) SELECT ?,?,?,?,? WHERE (SELECT COUNT(*) FROM hosts)<? ON CONFLICT DO NOTHING"
  ).bind(crypto.randomUUID(), label, hostname, keyHash, now, limit(env.MAX_HOSTS, 50, 100000)).run();
  if (!result.meta.changes) {
    const retry = await env.DB.prepare("SELECT * FROM hosts WHERE token_hash=? AND disabled=0").bind(keyHash).first();
    if (retry?.label === label) return reply({ ok: true, hostname });
    if (await env.DB.prepare("SELECT id FROM hosts WHERE label=?").bind(label).first()) fail(409, "name_taken", "Tên subdomain đã được sử dụng.");
    fail(429, "capacity", "Dịch vụ đã đạt hạn mức hostname. Liên hệ quản trị viên.");
  }
  return reply({ ok: true, hostname }, 201);
}

async function acme(request, env, remove) {
  const host = await ownedHost(request, env);
  if (!remove && !host.ipv4 && !host.ipv6) fail(409, "dns_not_ready", "Cập nhật DNS trước khi cấp SSL.");
  const input = await body(request, ["value"]);
  if (typeof input.value !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(input.value)) {
    fail(400, "invalid_challenge", "TXT phải là giá trị DNS-01 SHA-256 hợp lệ.");
  }
  const lock = crypto.randomUUID(), now = Date.now();
  const acquired = await env.DB.prepare(
    "INSERT INTO acme_locks(host_id,lock_id,expires_at) VALUES(?,?,?) ON CONFLICT(host_id) DO UPDATE SET lock_id=excluded.lock_id,expires_at=excluded.expires_at WHERE expires_at<=?"
  ).bind(host.id, lock, now + 30000, now).run();
  if (!acquired.meta.changes) fail(429, "acme_busy", "Đang xử lý xác thực SSL. Thử lại sau 30 giây.");
  const dnsName = "_acme-challenge." + host.hostname;
  const marker = "VPN20-ACME:" + host.id;
  try {
    if (!remove) {
      const bucket = "acme:" + host.id + ":" + Math.floor(now / 3600000);
      const rate = await env.DB.prepare(
        "INSERT INTO claim_buckets(bucket,attempts,expires_at) VALUES(?,1,?) ON CONFLICT(bucket) DO UPDATE SET attempts=attempts+1 WHERE attempts<120"
      ).bind(bucket, now + 3600000).run();
      if (!rate.meta.changes) fail(429, "acme_limit", "Đã đạt hạn mức xác thực SSL trong giờ.");
    }
    const records = await cloudflare(env, "?" + new URLSearchParams({ name: dnsName, type: "TXT", per_page: "100" }));
    if (!Array.isArray(records) || records.length >= 100) fail(409, "dns_record_conflict", "Quá nhiều bản ghi xác thực.");
    const matching = records.filter(r => r.comment === marker && r.content === input.value);
    if (remove) {
      for (const r of matching) {
        await cloudflare(env, "/" + r.id, "DELETE");
        await env.DB.prepare("DELETE FROM acme_records WHERE record_id=? AND host_id=?").bind(r.id, host.id).run();
      }
    } else {
      // Prune only this host's old challenges; never remove foreign TXT values.
      let live = records.filter(r => r.comment === marker).length;
      for (const r of records.filter(r => r.comment === marker && r.content !== input.value)) {
        const row = await env.DB.prepare("SELECT created_at FROM acme_records WHERE record_id=? AND host_id=?").bind(r.id, host.id).first();
        if (!row || row.created_at < now - 3600000) {
          await cloudflare(env, "/" + r.id, "DELETE");
          await env.DB.prepare("DELETE FROM acme_records WHERE record_id=?").bind(r.id).run();
          live--;
        }
      }
      if (!matching.length) {
        if (live >= 4) fail(429, "acme_limit", "Đã có nhiều yêu cầu cấp SSL đang chờ.");
        const record = await cloudflare(env, "", "POST", { type: "TXT", name: dnsName, content: input.value, ttl: 60, comment: marker });
        await env.DB.prepare("INSERT OR REPLACE INTO acme_records(record_id,host_id,value,created_at) VALUES(?,?,?,?)").bind(record.id, host.id, input.value, now).run();
      }
    }
    return reply({ ok: true, hostname: host.hostname });
  } finally {
    await env.DB.prepare("DELETE FROM acme_locks WHERE host_id=? AND lock_id=?").bind(host.id, lock).run();
  }
}

async function retire(request, env) {
  const host = await ownedHost(request, env);
  await body(request, []);
  const lock = crypto.randomUUID(), now = Date.now();
  const acquired = await env.DB.prepare("UPDATE hosts SET lock_id=?,lock_until=? WHERE id=? AND disabled=0 AND lock_until<=?").bind(lock, now + 30000, host.id, now).run();
  if (!acquired.meta.changes) fail(429, "busy", "Hostname đang được cập nhật. Thử lại sau 30 giây.");
  try {
    for (const type of ["A", "AAAA"]) {
      const records = await cloudflare(env, "?" + new URLSearchParams({ name: host.hostname, type, per_page: "100" }));
      for (const record of records) {
        if (record.comment === "VPN20-DDNS:" + host.id) await cloudflare(env, "/" + record.id, "DELETE");
      }
    }
    await env.DB.prepare("UPDATE hosts SET ipv4=NULL,ipv6=NULL,verified4=0,verified6=0 WHERE id=?").bind(host.id).run();
    // Reservation and key stay with their owner, so this name can be reused safely.
    return reply({ ok: true, hostname: host.hostname });
  } finally {
    await env.DB.prepare("UPDATE hosts SET lock_id=NULL,lock_until=0 WHERE id=? AND lock_id=?").bind(host.id, lock).run();
  }
}

export default {
  async fetch(request, env) {
    try {
      const path = new URL(request.url).pathname;
      const configured = Boolean(env.DB && /^[a-f0-9]{32}$/i.test(env.CF_ZONE_ID || "") &&
        env.CF_API_TOKEN?.length >= 20 && /^[A-Za-z0-9_-]{32,256}$/.test(env.ADMIN_TOKEN || ""));
      if (path === "/health" && request.method === "GET") return reply({ ok: configured, service: "vpn20-ddns", version: "0.2.0", domain: DOMAIN, features: ["claim", "dual-stack", "dns01"] }, configured ? 200 : 503);
      if (!configured) fail(503, "not_configured", "Cần cấu hình DB, CF_ZONE_ID, CF_API_TOKEN và ADMIN_TOKEN.");
      if (path === "/v1/ip" && request.method === "GET") return reply({ ok: true, ...publicIP(request.headers.get("CF-Connecting-IP")) });
      if (request.method !== "POST") fail(405, "method_not_allowed", "Dùng POST cho API này.");
      if (path === "/v1/claim") return await claim(request, env);
      if (path === "/v1/acme/present") return await acme(request, env, false);
      if (path === "/v1/acme/cleanup") return await acme(request, env, true);
      if (path === "/v1/retire") return await retire(request, env);
      if (path === "/v1/register") {
        await admin(request, env);
        const input = await body(request, ["name"]);
        const label = name(input.name === undefined ? `vp-${crypto.randomUUID().replaceAll("-", "").slice(0, 12)}` : input.name);
        const hostname = `${label}.${DOMAIN}`;
        const token = randomKey();
        const inserted = await env.DB.prepare(
          "INSERT INTO hosts(id,label,hostname,token_hash,created_at) VALUES(?,?,?,?,?) ON CONFLICT(label) DO NOTHING"
        ).bind(crypto.randomUUID(), label, hostname, await hash(token), Date.now()).run();
        if (!inserted.meta.changes) fail(409, "name_taken", "Hostname đã được cấp. Dùng khóa cũ hoặc nhờ quản trị viên đổi khóa.");
        return reply({ ok: true, hostname, update_token: token, update_path: "/v1/update" }, 201);
      }
      if (path === "/v1/update") return await update(request, env);
      const action = /^\/v1\/admin\/hosts\/([^/]+)\/(rotate|revoke)$/.exec(path);
      if (action) {
        await admin(request, env);
        const label = name(action[1]);
        const token = randomKey();
        const query = action[2] === "revoke"
          ? env.DB.prepare("UPDATE hosts SET disabled=1 WHERE label=?").bind(label)
          : env.DB.prepare("UPDATE hosts SET token_hash=?,disabled=0,verified4=0,verified6=0 WHERE label=?").bind(await hash(token), label);
        const result = await query.run();
        if (!result.meta.changes) fail(404, "not_found", "Hostname không tồn tại.");
        return reply(action[2] === "revoke" ? { ok: true, hostname: `${label}.${DOMAIN}`, disabled: true }
          : { ok: true, hostname: `${label}.${DOMAIN}`, update_token: token });
      }
      fail(404, "not_found", "Đường dẫn không tồn tại.");
    } catch (error) {
      if (error instanceof APIError) return reply({ ok: false, error: error.code, message: error.message }, error.status);
      // Do not expose request headers, body, database errors, or Cloudflare credentials.
      return reply({ ok: false, error: "service_error", message: "Dịch vụ lỗi. Kiểm tra D1, cấu hình và thử lại sau 30 giây." }, 503);
    }
  },
};
