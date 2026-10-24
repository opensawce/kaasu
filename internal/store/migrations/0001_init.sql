-- Money is always integer paise. Times are RFC 3339 UTC text. Never edit this file; add a new migration.

CREATE TABLE receiving_accounts (
  id                       TEXT PRIMARY KEY,
  vpa                      TEXT NOT NULL UNIQUE,
  bank                     TEXT NOT NULL,
  display_name             TEXT NOT NULL,
  auto_confirm_limit_paise INTEGER NOT NULL DEFAULT 0 CHECK (auto_confirm_limit_paise >= 0),
  active                   INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  created_at               TEXT NOT NULL
) STRICT;

CREATE TABLE orders (
  id                 TEXT PRIMARY KEY,
  account_id         TEXT NOT NULL REFERENCES receiving_accounts(id),
  base_amount_paise  INTEGER NOT NULL CHECK (base_amount_paise > 0),
  amount_paise       INTEGER NOT NULL CHECK (amount_paise > 0),
  currency           TEXT NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  state              TEXT NOT NULL CHECK (state IN ('created','queued','awaiting','matched','confirmed','review','rejected','cancelled','expired','late_paid','refunded','disputed')),
  reference          TEXT,
  note               TEXT,
  idempotency_key    TEXT UNIQUE,
  request_hash       TEXT,
  expires_at         TEXT NOT NULL,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  confirmed_at       TEXT
) STRICT;
CREATE INDEX orders_account_state ON orders(account_id, state);

CREATE TABLE amount_reservations (
  account_id     TEXT NOT NULL REFERENCES receiving_accounts(id),
  amount_paise   INTEGER NOT NULL CHECK (amount_paise > 0),
  order_id       TEXT NOT NULL UNIQUE REFERENCES orders(id),
  reserved_until TEXT NOT NULL,
  PRIMARY KEY (account_id, amount_paise)
) STRICT;

CREATE TABLE devices (
  id           TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('android','apple','windows','email')),
  public_key   BLOB NOT NULL,
  paired_at    TEXT NOT NULL,
  last_seen_at TEXT,
  revoked_at   TEXT
) STRICT;

CREATE TABLE nonces (
  device_id TEXT NOT NULL REFERENCES devices(id),
  nonce     TEXT NOT NULL,
  seen_at   TEXT NOT NULL,
  PRIMARY KEY (device_id, nonce)
) STRICT;

CREATE TABLE evidence (
  id          TEXT PRIMARY KEY,
  source      TEXT NOT NULL CHECK (source IN ('device','email','manual')),
  device_id   TEXT REFERENCES devices(id),
  received_at TEXT NOT NULL,
  raw_hash    TEXT NOT NULL UNIQUE,
  body        BLOB NOT NULL
) STRICT;

CREATE TABLE alerts (
  id            TEXT PRIMARY KEY,
  evidence_id   TEXT NOT NULL REFERENCES evidence(id),
  bank          TEXT,
  amount_paise  INTEGER NOT NULL CHECK (amount_paise > 0),
  utr           TEXT UNIQUE,
  payer_masked  TEXT,
  account_hint  TEXT,
  occurred_at   TEXT NOT NULL,
  parsed_at     TEXT NOT NULL,
  parser        TEXT
) STRICT;

CREATE TABLE matches (
  id         TEXT PRIMARY KEY,
  alert_id   TEXT NOT NULL UNIQUE REFERENCES alerts(id),
  order_id   TEXT NOT NULL REFERENCES orders(id),
  score      INTEGER NOT NULL CHECK (score BETWEEN 0 AND 100),
  decision   TEXT NOT NULL CHECK (decision IN ('auto','review','rejected')),
  decided_by TEXT,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE refunds (
  id           TEXT PRIMARY KEY,
  order_id     TEXT NOT NULL REFERENCES orders(id),
  amount_paise INTEGER NOT NULL CHECK (amount_paise > 0),
  utr          TEXT,
  note         TEXT,
  created_at   TEXT NOT NULL
) STRICT;

CREATE TABLE ledger (
  seq       INTEGER PRIMARY KEY AUTOINCREMENT,
  ts        TEXT NOT NULL,
  kind      TEXT NOT NULL,
  order_id  TEXT,
  actor     TEXT NOT NULL,
  payload   TEXT NOT NULL,
  prev_hash TEXT NOT NULL,
  hash      TEXT NOT NULL UNIQUE
) STRICT;

CREATE TABLE outbox (
  id              TEXT PRIMARY KEY,
  event_id        TEXT NOT NULL UNIQUE,
  kind            TEXT NOT NULL,
  payload         TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','delivered','dead')),
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  delivered_at    TEXT
) STRICT;
CREATE INDEX outbox_due ON outbox(status, next_attempt_at);
