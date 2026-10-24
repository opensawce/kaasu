CREATE TABLE statement_rows (
  id           TEXT PRIMARY KEY,
  batch_id     TEXT NOT NULL,
  row_no       INTEGER NOT NULL,
  posted_on    TEXT NOT NULL,
  description  TEXT NOT NULL,
  credit_paise INTEGER NOT NULL DEFAULT 0 CHECK (credit_paise >= 0),
  debit_paise  INTEGER NOT NULL DEFAULT 0 CHECK (debit_paise >= 0),
  utr          TEXT,
  UNIQUE (batch_id, row_no)
) STRICT;
CREATE INDEX statement_rows_utr ON statement_rows(utr);
CREATE TABLE statement_batches (
  id         TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES receiving_accounts(id),
  file_hash  TEXT NOT NULL,
  rows       INTEGER NOT NULL,
  imported_at TEXT NOT NULL,
  UNIQUE (account_id, file_hash)
) STRICT;
