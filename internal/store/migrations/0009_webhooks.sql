CREATE TABLE webhook_deliveries (
  id          TEXT PRIMARY KEY,
  outbox_id   TEXT NOT NULL REFERENCES outbox(id),
  attempt     INTEGER NOT NULL,
  status_code INTEGER,
  error       TEXT,
  at          TEXT NOT NULL
) STRICT;
