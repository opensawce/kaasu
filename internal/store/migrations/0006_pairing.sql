CREATE TABLE pair_tokens (
  token_hash TEXT PRIMARY KEY,
  expires_at TEXT NOT NULL,
  used_at    TEXT,
  created_at TEXT NOT NULL
) STRICT;
