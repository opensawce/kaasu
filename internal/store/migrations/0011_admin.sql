CREATE TABLE admins (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  role        TEXT NOT NULL CHECK (role IN ('owner','staff')),
  totp_step   INTEGER NOT NULL DEFAULT -1,
  created_at  TEXT NOT NULL,
  disabled_at TEXT
) STRICT;
CREATE TABLE sessions (
  id_hash      TEXT PRIMARY KEY,
  admin_id     TEXT NOT NULL REFERENCES admins(id),
  created_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  fresh_until  TEXT NOT NULL,
  csrf_hash    TEXT NOT NULL
) STRICT;
CREATE TABLE auth_failures (
  key          TEXT PRIMARY KEY,
  failures     INTEGER NOT NULL,
  locked_until TEXT
) STRICT;
