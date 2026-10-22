CREATE TABLE IF NOT EXISTS hosts (
  id TEXT PRIMARY KEY,
  label TEXT NOT NULL UNIQUE,
  hostname TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL UNIQUE,
  disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
  ipv4 TEXT,
  ipv6 TEXT,
  verified4 INTEGER NOT NULL DEFAULT 0,
  verified6 INTEGER NOT NULL DEFAULT 0,
  last_attempt INTEGER NOT NULL DEFAULT 0,
  lock_id TEXT,
  lock_until INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
-- Additive migration: safe to run again on the original v0.1 database.
CREATE TABLE IF NOT EXISTS claim_buckets (
  bucket TEXT PRIMARY KEY,
  attempts INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS claim_buckets_expiry ON claim_buckets(expires_at);
CREATE TABLE IF NOT EXISTS acme_locks (
  host_id TEXT PRIMARY KEY REFERENCES hosts(id),
  lock_id TEXT NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS acme_records (
  record_id TEXT PRIMARY KEY,
  host_id TEXT NOT NULL REFERENCES hosts(id),
  value TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(host_id, value)
);
