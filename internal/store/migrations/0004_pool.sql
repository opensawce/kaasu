-- Queued orders wait here until an amount slot frees up. Settings holds small key/value config (grace minutes, webhook URL, ...).
ALTER TABLE orders ADD COLUMN queued_at TEXT;

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
