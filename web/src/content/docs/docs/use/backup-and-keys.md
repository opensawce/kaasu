---
title: Backups and keys
description: What to back up, how to rotate the master key, and how to check history.
---

## What to back up

Everything Kaasu knows is in one SQLite file (`kaasu.db`, set by `KAASU_DATABASE_PATH`) next to its `-wal` and `-shm` files, plus your **master key**. Without the key the secrets inside the file cannot be read.

- Copy the database only while Kaasu is stopped, or use SQLite's own online backup (`sqlite3 kaasu.db ".backup backup.db"`).
- Keep the master key somewhere else, offline. Never store it next to the backup.
- Automatic encrypted backups with a weekly restore test are planned; `kaasu backup` is a placeholder today.

## Rotating the master key

```bash
export KAASU_NEW_MASTER_KEY=$(kaasu keygen)
kaasu rotate-key
```

Every stored secret is re-wrapped under the new key in one transaction: if any fails, nothing changes. After it succeeds, point `KAASU_MASTER_KEY` at the new key and restart.

## Check the ledger

`kaasu verify` walks the chained history and tells you the first row that does not match. Run it after a restore and now and then.

## Revoking things

- API keys are stored as hashes and cannot be shown again. Make a new one with `kaasu apikey new <label>` and stop using the old one.
- A lost phone: device revocation is built (a revoked device's next request is refused immediately), but there is no command or screen for it yet. Until there is, stop the service and ask in the issue tracker for the manual steps rather than editing the database yourself.
