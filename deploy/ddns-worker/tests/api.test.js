import { test, beforeEach, after } from "node:test";
import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { readFileSync } from "node:fs";
import worker from "../src/index.js";

// Real SQLite for the D1 SQL and constraints, with Cloudflare DNS API mocked.
const originalFetch = globalThis.fetch;
const originalNow = Date.now;
let sqlite, records, dnsCalls, now, dnsFailure, sequence;
const env = { CF_ZONE_ID: "a".repeat(32), CF_API_TOKEN: "test-cloudflare-secret-never-return", ADMIN_TOKEN: "admin-test-" + "a".repeat(54) };

function database(db) {
  return { prepare(sql) {
    let values = [];
    return {
      bind(...items) { values = items; return this; },
      async first() { return db.prepare(sql).get(...values) || null; },
      async run() {
        const result = db.prepare(sql).run(...values);
        return { success: true, results: [], meta: { changes: Number(result.changes) } };
      },
    };
  } };
}

beforeEach(() => {
  sqlite?.close();
  sqlite = new DatabaseSync(":memory:");
  sqlite.exec(readFileSync(new URL("../schema.sql", import.meta.url), "utf8"));
  env.DB = database(sqlite);
  delete env.MAX_HOSTS; delete env.CLAIMS_PER_IP_DAY; delete env.SELF_REGISTRATION;
  records = [];
  dnsCalls = [];
  sequence = 0;
  dnsFailure = false;
  now = 1_790_000_000_000;
  Date.now = () => now;
  globalThis.fetch = async (url, options) => {
    const parsed = new URL(url);
    assert.equal(parsed.origin, "https://api.cloudflare.com");
    assert.ok(parsed.pathname.startsWith(`/client/v4/zones/${env.CF_ZONE_ID}/dns_records`));
    assert.equal(options.headers.Authorization, `Bearer ${env.CF_API_TOKEN}`);
    const data = options.body ? JSON.parse(options.body) : null;
    dnsCalls.push({ method: options.method, path: parsed.pathname, data });
    if (dnsFailure) return Response.json({ success: false, errors: [{ code: 10000, message: "not authorized" }] }, { status: 403 });
    let result;
    if (options.method === "GET") {
      result = records.filter(r => r.name === parsed.searchParams.get("name") && (!parsed.searchParams.has("type") || r.type === parsed.searchParams.get("type")));
    } else if (options.method === "POST") {
      assert.ok(data.name.endsWith(".vpn.20.com.vn"));
      result = { ...data, id: `record-${++sequence}` };
      records.push(result);
    } else if (options.method === "PATCH") {
      const id = parsed.pathname.split("/").at(-1);
      const index = records.findIndex(r => r.id === id);
      assert.ok(index >= 0);
      result = records[index] = { ...records[index], ...data };
    } else if (options.method === "DELETE") {
      const id = parsed.pathname.split("/").at(-1);
      records = records.filter(r => r.id !== id);
      result = { id };
    } else assert.fail("Unexpected Cloudflare mutation");
    return Response.json({ success: true, result });
  };
});

after(() => { globalThis.fetch = originalFetch; Date.now = originalNow; sqlite?.close(); });
test("self-claim is idempotent with a client-saved key and never returns secrets", async () => {
  const token = "1".repeat(64);
  let result = await call("/v1/claim", { token, data: { name: "Office" } });
  assert.equal(result.status, 201);
  assert.equal(result.body.hostname, "office.vpn.20.com.vn");
  assert.equal("update_token" in result.body, false);
  assert.equal(JSON.stringify(result.body).includes(token), false);
  assert.notEqual(sqlite.prepare("SELECT token_hash FROM hosts").get().token_hash, token);
  result = await call("/v1/claim", { token, data: { name: "office" } });
  assert.equal(result.status, 200);
  assert.equal(sqlite.prepare("SELECT COUNT(*) AS n FROM hosts").get().n, 1);
  assert.equal((await call("/v1/claim", { token, data: { name: "different" } })).status, 409);
  assert.equal((await call("/v1/claim", { token: "2".repeat(64), data: { name: "office" } })).status, 409);
  assert.equal((await call("/v1/claim", { token: "short", data: { name: "other" } })).status, 401);
  assert.equal((await call("/v1/claim", { token, data: { name: "foreign.example.com" } })).status, 400);
});

test("self-registration enforces per-network and global capacity, with retries exempt", async () => {
  env.MAX_HOSTS = "2";
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "one" } })).status, 201);
  assert.equal((await call("/v1/claim", { token: "2".repeat(64), data: { name: "two" } })).status, 201);
  assert.equal((await call("/v1/claim", { token: "3".repeat(64), data: { name: "three" } })).body.error, "capacity");
  assert.equal((await call("/v1/claim", { token: "4".repeat(64), data: { name: "four" } })).body.error, "claim_limit");
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "one" } })).status, 200);
  env.SELF_REGISTRATION = "false";
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "one" } })).status, 403);
});

test("self-claim refuses an existing manually managed DNS name, including CNAME", async () => {
  records.push({ id: "foreign", name: "office.vpn.20.com.vn", type: "CNAME", content: "another.example" });
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "office" } })).status, 409);
  assert.equal(sqlite.prepare("SELECT COUNT(*) AS n FROM hosts").get().n, 0);
  assert.equal(records[0].id, "foreign");
});

test("updates both families under one lease and removes stale AAAA explicitly", async () => {
  const host = await register("dual");
  let result = await call("/v1/update", { token: host.update_token, data: { ipv4: "8.8.8.8", ipv6: "2606:4700:4700::1111" } });
  assert.equal(result.status, 200);
  assert.equal(result.body.records.length, 2);
  assert.deepEqual(records.map(r => r.type).sort(), ["A", "AAAA"]);
  now += 31000;
  result = await call("/v1/update", { token: host.update_token, data: { ipv4: "1.1.1.1", ipv6: null } });
  assert.equal(result.status, 200);
  assert.deepEqual(records.map(r => r.type), ["A"]);
  assert.equal(sqlite.prepare("SELECT ipv6 FROM hosts").get().ipv6, null);
  assert.equal((await call("/v1/update", { token: host.update_token, data: { ip: "8.8.8.8", ipv4: "1.1.1.1" } })).status, 400);
  assert.equal((await call("/v1/update", { token: host.update_token, data: { ipv4: null, ipv6: null } })).status, 400);
  assert.equal((await call("/v1/update", { token: host.update_token, data: { ipv4: "2606:4700:4700::1111" } })).status, 400);
});

test("DNS-01 only writes exact own-host TXT and preserves foreign records", async () => {
  const first = await register("one");
  const second = await register("two");
  const value = "a".repeat(43);
  assert.equal((await call("/v1/acme/present", { token: first.update_token, data: { value } })).status, 409);
  await call("/v1/update", { token: first.update_token, data: {} });
  records.push({ id: "foreign", name: "_acme-challenge.one.vpn.20.com.vn", type: "TXT", content: value, comment: "manual" });
  assert.equal((await call("/v1/acme/present", { token: first.update_token, data: { value, hostname: second.hostname } })).status, 400);
  assert.equal((await call("/v1/acme/present", { token: first.update_token, data: { value: "invalid" } })).status, 400);
  assert.equal((await call("/v1/acme/present", { token: first.update_token, data: { value } })).status, 200);
  assert.equal((await call("/v1/acme/present", { token: first.update_token, data: { value } })).status, 200);
  let own = records.filter(r => r.type === "TXT" && r.comment.startsWith("VPN20-ACME:"));
  assert.equal(own.length, 1);
  assert.equal(own[0].name, "_acme-challenge.one.vpn.20.com.vn");
  assert.equal((await call("/v1/acme/cleanup", { token: first.update_token, data: { value } })).status, 200);
  assert.equal(records.some(r => r.id === "foreign"), true);
  assert.equal(sqlite.prepare("SELECT COUNT(*) AS n FROM acme_records").get().n, 0);
});

test("DNS-01 caps outstanding challenges and prunes only expired owned values", async () => {
  const host = await register("cert-test");
  await call("/v1/update", { token: host.update_token, data: {} });
  for (const char of ["a", "b", "c", "d"]) {
    assert.equal((await call("/v1/acme/present", { token: host.update_token, data: { value: char.repeat(43) } })).status, 200);
  }
  assert.equal((await call("/v1/acme/present", { token: host.update_token, data: { value: "e".repeat(43) } })).status, 429);
  now += 3601000;
  assert.equal((await call("/v1/acme/present", { token: host.update_token, data: { value: "e".repeat(43) } })).status, 200);
  assert.equal(records.filter(r => r.type === "TXT").length, 1);
  await call("/v1/admin/hosts/cert-test/revoke", { token: env.ADMIN_TOKEN });
  assert.equal((await call("/v1/acme/present", { token: host.update_token, data: { value: "f".repeat(43) } })).status, 401);
});

test("retiring removes only owned address records and preserves the name reservation", async () => {
  const host = await register("old");
  await call("/v1/update", { token: host.update_token, data: {} });
  records.push({ id: "manual", name: host.hostname, type: "AAAA", content: "2606:4700:4700::1111", comment: "foreign" });
  assert.equal((await call("/v1/retire", { token: host.update_token })).status, 200);
  assert.deepEqual(records.map(r => r.id), ["manual"]);
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "old" } })).status, 409);
  assert.equal((await call("/v1/claim", { token: host.update_token, data: { name: "old" } })).status, 200);
});

test("health advertises protocol capabilities and IP discovery returns only the source IP", async () => {
  const health = await call("/health", { method: "GET" });
  assert.equal(health.body.version, "0.2.1");
  assert.ok(health.body.features.includes("dns01"));
  const ip = await call("/v1/ip", { method: "GET", ip: "8.8.4.4" });
  assert.deepEqual(ip.body, { ok: true, ip: "8.8.4.4", type: "A" });
});

test("pasting the new Worker upgrades original tables without losing hosts or keys", async () => {
  const host = await register("existing");
  await call("/v1/update", { token: host.update_token, data: {} });
  const before = sqlite.prepare("SELECT * FROM hosts").get();
  sqlite.exec("DROP TABLE acme_records; DROP TABLE acme_locks; DROP TABLE claim_buckets");
  env.DB = database(sqlite); // New deployment/isolate, original v0.1 database.
  assert.equal((await call("/health", { method: "GET" })).status, 200);
  assert.deepEqual(sqlite.prepare("SELECT * FROM hosts").get(), before);
  assert.equal((await call("/v1/claim", { token: host.update_token, data: { name: "existing" } })).status, 200);
  assert.equal((await call("/v1/claim", { token: "2".repeat(64), data: { name: "new-machine" } })).status, 201);
  assert.equal((await call("/v1/acme/present", { token: host.update_token, data: { value: "a".repeat(43) } })).status, 200);
  assert.equal((await call("/v1/acme/cleanup", { token: host.update_token, data: { value: "a".repeat(43) } })).status, 200);
});

test("health checks real D1 schema and recovers after a failed readiness check", async () => {
  sqlite.exec("DROP TABLE acme_records; DROP TABLE acme_locks; DROP TABLE claim_buckets; DROP TABLE hosts");
  let result = await call("/health", { method: "GET" });
  assert.equal(result.status, 503);
  assert.equal(result.body.error, "schema_outdated");
  assert.match(result.body.message, /schema.sql/);
  assert.equal(dnsCalls.length, 0);
  sqlite.exec(readFileSync(new URL("../schema.sql", import.meta.url), "utf8"));
  result = await call("/health", { method: "GET" });
  assert.equal(result.status, 200);
  assert.equal(result.body.ok, true);
});

test("health rejects incompatible auxiliary columns instead of claiming readiness", async () => {
  sqlite.exec("DROP TABLE claim_buckets; CREATE TABLE claim_buckets (bucket TEXT PRIMARY KEY)");
  const result = await call("/health", { method: "GET" });
  assert.equal(result.status, 503);
  assert.equal(result.body.error, "schema_outdated");
});

test("D1 outage returns a safe actionable error and can recover", async () => {
  env.DB = { prepare() { throw new Error("D1_ERROR: quota exceeded SECRET"); } };
  const result = await call("/health", { method: "GET" });
  assert.equal(result.status, 503);
  assert.equal(result.body.error, "database_unavailable");
  assert.equal(JSON.stringify(result.body).includes("SECRET"), false);
  env.DB = database(sqlite);
  assert.equal((await call("/health", { method: "GET" })).status, 200);
});

test("Cloudflare network and authorization failures are distinguishable and retryable", async () => {
  env.CLAIMS_PER_IP_DAY = "1";
  const workingFetch = globalThis.fetch;
  globalThis.fetch = async () => { throw new Error("timeout SECRET"); };
  let result = await call("/v1/claim", { token: "1".repeat(64), data: { name: "office" } });
  assert.equal(result.status, 502);
  assert.equal(result.body.error, "dns_api_unavailable");
  assert.equal(JSON.stringify(result.body).includes("SECRET"), false);
  globalThis.fetch = workingFetch;
  dnsFailure = true;
  result = await call("/v1/claim", { token: "1".repeat(64), data: { name: "office" } });
  assert.equal(result.body.error, "dns_auth_error");
  dnsFailure = false;
  assert.equal((await call("/v1/claim", { token: "1".repeat(64), data: { name: "office" } })).status, 201);
});

test("slow upstream operations keep their lease beyond the old 30-second expiry", async () => {
  const host = await register("slow");
  const workingFetch = globalThis.fetch;
  let release, entered;
  const gate = new Promise(resolve => { release = resolve; });
  const started = new Promise(resolve => { entered = resolve; });
  globalThis.fetch = async (...args) => { entered(); await gate; return workingFetch(...args); };
  const first = call("/v1/update", { token: host.update_token, data: {} });
  await started;
  now += 31000;
  const competing = await call("/v1/update", { token: host.update_token, data: {} });
  assert.equal(competing.status, 429);
  release();
  assert.equal((await first).status, 200);
  assert.equal(records.length, 1);
});

async function call(path, { token, data, ip = "8.8.8.8", method = "POST", headers = {} } = {}) {
  const request = new Request(`https://ddns.example.test${path}`, {
    method,
    headers: { "CF-Connecting-IP": ip, ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(data === undefined ? {} : { "Content-Type": "application/json" }), ...headers },
    ...(data === undefined ? {} : { body: JSON.stringify(data) }),
  });
  const response = await worker.fetch(request, env);
  const result = await response.json();
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(JSON.stringify(result).includes(env.CF_API_TOKEN), false);
  assert.equal(JSON.stringify(result).includes(env.ADMIN_TOKEN), false);
  return { status: response.status, body: result, headers: response.headers };
}

async function register(label) {
  const result = await call("/v1/register", { token: env.ADMIN_TOKEN, data: label === undefined ? {} : { name: label } });
  assert.equal(result.status, 201);
  return result.body;
}

test("requires administrator authorization to allocate names; rejects cross-zone names", async () => {
  assert.equal((await call("/v1/register", { data: { name: "office" } })).status, 401);
  assert.equal((await call("/v1/register", { token: "b".repeat(64), data: { name: "office" } })).status, 401);
  for (const name of ["20.com.vn", "office.vpn.20.com.vn", "-bad", "bad-", "admin", "a".repeat(64), "x/other", "x\nother"]) {
    assert.equal((await call("/v1/register", { token: env.ADMIN_TOKEN, data: { name } })).status, 400);
  }
  assert.equal(dnsCalls.length, 0);
});

test("persists a unique hostname and hashes its update key", async () => {
  const host = await register("Office");
  assert.equal(host.hostname, "office.vpn.20.com.vn");
  assert.equal(host.update_token.length, 64);
  const row = sqlite.prepare("SELECT * FROM hosts").get();
  assert.notEqual(row.token_hash, host.update_token);
  assert.equal(row.token_hash.length, 64);
  assert.equal((await call("/v1/register", { token: env.ADMIN_TOKEN, data: { name: "office" } })).status, 409);
  const generated = await register();
  assert.match(generated.hostname, /^vp-[a-f0-9]{12}\.vpn\.20\.com\.vn$/);
});

test("updates only the hostname bound to the key, creates DNS-only A and AAAA records", async () => {
  const first = await register("one");
  const second = await register("two");
  assert.equal((await call("/v1/update", { token: first.update_token, data: { hostname: second.hostname } })).status, 400);
  let result = await call("/v1/update", { token: first.update_token, data: {} });
  assert.equal(result.status, 200);
  assert.equal(records[0].name, first.hostname);
  assert.equal(records[0].type, "A");
  assert.equal(records[0].content, "8.8.8.8");
  assert.equal(records[0].proxied, false);
  assert.equal(records[0].ttl, 60);
  now += 31_000;
  result = await call("/v1/update", { token: first.update_token, data: { ip: "2606:4700:4700::1111" } });
  assert.equal(result.status, 200);
  assert.deepEqual(records.map(r => r.type).sort(), ["A", "AAAA"]);
  assert.equal(records.every(r => r.name === first.hostname), true);
  assert.equal((await call("/v1/update", { token: "c".repeat(64), data: {} })).status, 401);
});

test("rejects non-public addresses before any DNS operation", async () => {
  const host = await register("addresses");
  for (const ip of ["127.0.0.1", "192.168.1.100", "100.64.1.1", "10.0.0.2", "0.0.0.0", "203.0.113.1", "256.1.1.1", "::1", "2::", "fc00::1", "fe80::1", "2001:db8::1", "example.com", "8.8.8.8/32"]) {
    assert.equal((await call("/v1/update", { token: host.update_token, data: { ip } })).status, 400, ip);
  }
  assert.equal(dnsCalls.length, 0);
});

test("rate-limits and serializes simultaneous updates", async () => {
  const host = await register("concurrent");
  const results = await Promise.all([call("/v1/update", { token: host.update_token, data: {} }), call("/v1/update", { token: host.update_token, data: {} })]);
  assert.deepEqual(results.map(r => r.status).sort(), [200, 429]);
  assert.equal(records.length, 1);
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).headers.get("Retry-After"), "30");
});

test("does not overwrite an existing DNS record owned by another service", async () => {
  const host = await register("existing");
  records.push({ id: "foreign", name: host.hostname, type: "A", content: "1.1.1.1", comment: "manually managed", ttl: 300, proxied: true });
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).status, 409);
  assert.equal(records[0].content, "1.1.1.1");
  assert.deepEqual(dnsCalls.map(c => c.method), ["GET"]);
});

test("keeps successful data on Cloudflare failure; retries recover without losing the hostname", async () => {
  const host = await register("recover");
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).status, 200);
  now += 31_000;
  dnsFailure = true;
  assert.equal((await call("/v1/update", { token: host.update_token, data: { ip: "1.1.1.1" } })).status, 502);
  assert.equal(records[0].content, "8.8.8.8");
  assert.equal(sqlite.prepare("SELECT ipv4 FROM hosts").get().ipv4, "8.8.8.8");
  now += 31_000;
  dnsFailure = false;
  assert.equal((await call("/v1/update", { token: host.update_token, data: { ip: "1.1.1.1" } })).status, 200);
  assert.equal(records.length, 1);
  assert.equal(records[0].content, "1.1.1.1");
});

test("skips unchanged DNS calls, then reconciles a deleted owned record", async () => {
  const host = await register("reconcile");
  await call("/v1/update", { token: host.update_token, data: {} });
  const count = dnsCalls.length;
  now += 31_000;
  const unchanged = await call("/v1/update", { token: host.update_token, data: {} });
  assert.equal(unchanged.body.changed, false);
  assert.equal(dnsCalls.length, count);
  records = [];
  now += 16 * 60_000;
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).status, 200);
  assert.equal(records.length, 1);
});

test("revokes keys and rotates without changing other hosts or deleting DNS", async () => {
  const host = await register("keytest");
  const other = await register("unaffected");
  await call("/v1/update", { token: host.update_token, data: {} });
  assert.equal((await call("/v1/admin/hosts/keytest/revoke", { token: other.update_token })).status, 401);
  assert.equal((await call("/v1/admin/hosts/keytest/revoke", { token: env.ADMIN_TOKEN })).status, 200);
  now += 31_000;
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).status, 401);
  const rotated = await call("/v1/admin/hosts/keytest/rotate", { token: env.ADMIN_TOKEN });
  assert.equal(rotated.status, 200);
  assert.notEqual(rotated.body.update_token, host.update_token);
  assert.equal((await call("/v1/update", { token: host.update_token, data: {} })).status, 401);
  assert.equal((await call("/v1/update", { token: rotated.body.update_token, data: {} })).status, 200);
  assert.equal((await call("/v1/update", { token: other.update_token, data: {} })).status, 200);
  assert.equal(records.length, 2);
});

test("fails closed on missing configuration and rejects GET updates", async () => {
  const missing = await worker.fetch(new Request("https://ddns.example.test/health"), {});
  assert.equal(missing.status, 503);
  assert.equal((await call("/v1/update", { method: "GET" })).status, 405);
  assert.equal(dnsCalls.length, 0);
});
