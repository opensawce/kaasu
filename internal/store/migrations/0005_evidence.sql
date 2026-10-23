CREATE TABLE alert_sources (
  alert_id    TEXT NOT NULL REFERENCES alerts(id),
  evidence_id TEXT NOT NULL REFERENCES evidence(id),
  PRIMARY KEY (alert_id, evidence_id)
) STRICT;
ALTER TABLE alerts ADD COLUMN fingerprint TEXT;
CREATE INDEX alerts_fingerprint ON alerts(fingerprint);
