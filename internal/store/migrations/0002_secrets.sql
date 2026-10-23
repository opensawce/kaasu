CREATE TABLE secrets (
  name       TEXT PRIMARY KEY,
  blob       TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE api_keys (
  id         TEXT PRIMARY KEY,
  label      TEXT NOT NULL,
  prefix     TEXT NOT NULL,
  key_hash   TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  revoked_at TEXT
) STRICT;
