CREATE TABLE jobs (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,
  payload      TEXT NOT NULL DEFAULT '{}',
  run_at       TEXT NOT NULL,
  attempts     INTEGER NOT NULL DEFAULT 0,
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','done','dead')),
  last_error   TEXT,
  locked_until TEXT,
  created_at   TEXT NOT NULL
) STRICT;
CREATE INDEX jobs_due ON jobs(status, run_at);
CREATE TABLE recurring (
  kind          TEXT PRIMARY KEY,
  every_seconds INTEGER NOT NULL CHECK (every_seconds > 0),
  next_run_at   TEXT NOT NULL
) STRICT;
