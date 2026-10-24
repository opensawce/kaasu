---
title: Configuration
description: Every setting, its default and where it comes from.
---

Settings come from three places, later ones winning: built-in defaults, a JSON file named by `KAASU_CONFIG`, then environment variables. An unknown key in the file is an error, so a typo cannot be silently ignored.

| Environment variable | File key | Default | Meaning |
|---|---|---|---|
| `KAASU_PUBLIC_ADDR` | `public_addr` | `:8080` | Customer pages and the order API. |
| `KAASU_INGEST_ADDR` | `ingest_addr` | `:8443` | Signed device alerts. |
| `KAASU_ADMIN_ADDR` | `admin_addr` | `127.0.0.1:9000` | Dashboard and metrics. Loopback by default. |
| `KAASU_PAIRING_HOST` | `pairing_host` | the ingest address | The host:port phones are told to reach for ingest (put your proxy address here). |
| `KAASU_DATABASE_PATH` | `database_path` | `kaasu.db` | The SQLite file. |
| `KAASU_LOG_LEVEL` | `log_level` | `info` | `debug`, `info`, `warn` or `error`. |
| `KAASU_ADMIN_ALLOW_NON_LOOPBACK` | `admin_allow_non_loopback` | off | Allows the admin address to be non-loopback. Only on a private network. |
| `KAASU_CONFIG` | | none | Path to the JSON config file. |

## Secrets

| Variable | Meaning |
|---|---|
| `KAASU_MASTER_KEY` | The 32-byte master key from `kaasu keygen`. On macOS it can also come from the login keychain; under systemd or Compose it can be a credential file. |
| `KAASU_NEW_MASTER_KEY` | Used only by `kaasu rotate-key`. |
| `KAASU_DISABLE_KEYCHAIN=1` | Never read the macOS keychain (containers, CI, tests). |

## Ports and TLS

Kaasu serves plain HTTP. Put a reverse proxy or your host's TLS in front of the public and ingest ports, and never expose the admin port. The default `:8080` stands in for the usual public `:443`; terminating TLS on 443 is left to the proxy.

## Limits worth knowing

Request bodies are limited to 64 KB, each request has a 15 second budget, and each listener rate-limits per client address (public 200 a second with a burst of 400, ingest 50 and 100, admin 10 and 30). The server shuts down gracefully, finishing in-flight requests, and exits non-zero if any listener fails.
