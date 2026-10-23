CREATE TABLE ai_usage (
  id              TEXT PRIMARY KEY,
  at              TEXT NOT NULL,
  feature         TEXT NOT NULL,
  provider        TEXT NOT NULL,
  model           TEXT NOT NULL,
  input_tokens    INTEGER NOT NULL DEFAULT 0,
  output_tokens   INTEGER NOT NULL DEFAULT 0,
  cost_micro_inr  INTEGER NOT NULL DEFAULT 0
) STRICT;
