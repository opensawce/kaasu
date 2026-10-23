CREATE TABLE payment_links (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES receiving_accounts(id),
  kind         TEXT NOT NULL CHECK (kind IN ('link','counter')),
  amount_paise INTEGER CHECK (amount_paise IS NULL OR amount_paise > 0),
  note         TEXT,
  expires_at   TEXT,
  created_at   TEXT NOT NULL,
  disabled_at  TEXT
) STRICT;
