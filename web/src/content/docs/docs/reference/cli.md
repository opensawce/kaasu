---
title: Command line
description: Every kaasu command, what it does and what it prints.
---

Commands read the same settings as the server: `KAASU_DATABASE_PATH`, `KAASU_MASTER_KEY` and the rest in [Configuration](/docs/reference/configuration/). Run them as the user that owns the database.

| Command | What it does |
|---|---|
| `kaasu serve` | Starts the three listeners and the background jobs. It takes no flags; configuration comes from the environment or `KAASU_CONFIG`. An unknown argument is refused. |
| `kaasu migrate` | Applies database migrations and exits. The server does this at start too. |
| `kaasu keygen` | Prints a fresh 32-byte master key. Does not touch the database. |
| `kaasu rotate-key` | Re-wraps every secret under `KAASU_NEW_MASTER_KEY`, all or nothing. |
| `kaasu account add --vpa <id> --bank <name> --name <display name> [--limit-rupees N]` | Adds a receiving UPI account. `--limit-rupees` is the auto-confirm ceiling; 0 means every payment waits for you. |
| `kaasu apikey new <label>` | Creates an API key and prints it once. Only a hash is stored. |
| `kaasu admin init --name <name> [--role owner\|staff]` | Creates a person and prints their authenticator link once. Default role is owner. |
| `kaasu verify` | Walks the ledger hash chain and reports the first broken row. |
| `kaasu doctor` | Checks config, master key, database, pending migrations and the ledger. It only reads. |
| `kaasu healthcheck` | Exits 0 if the admin listener answers `/healthz` (used by the Docker image). |
| `kaasu sim --list` | Lists the payment simulator scenarios. |
| `kaasu version` | Prints the version. |
| `kaasu backup`, `kaasu pair` | Placeholders; not implemented yet. |

## Exit codes

`0` success, `1` the command ran and failed, `2` the command was used wrongly (the usage line is printed).

## Examples

```bash
kaasu account add --vpa sharma.sweets@okhdfcbank --bank HDFC --name "Sharma Sweets" --limit-rupees 5000
kaasu apikey new website
kaasu admin init --name priya --role owner
kaasu doctor
```
