---
title: Operations
description: Health checks, logs, metrics and the background jobs.
---

## Health

- `GET /healthz` on each listener returns `{"status":"ok"}`. Docker uses `kaasu healthcheck`, which asks the admin listener.
- `kaasu doctor` is the deeper check: configuration, master key, database, pending migrations and the ledger. It never changes anything.

## Logs

Logs are JSON lines on stderr. Every request has an `X-Request-Id` that appears in its log lines. Before a line is written, email addresses, UPI addresses, 12-digit UTRs and long account numbers are masked (for example `ra***@okicici`, `***5678`), including inside error text.

## Metrics

`/metrics` on the **admin** port serves Prometheus metrics. It is not on the public or ingest ports.

## Background jobs

A small persistent scheduler runs inside the server. Jobs survive restarts and a crashed job is retried.

| Job | Every | Does |
|---|---|---|
| `orders.expire` | 5 s | Expires orders whose time is up. |
| `pool.sweep` | 1 min | Gives freed amounts to queued orders. |
| `webhooks.deliver` | 5 s | Sends pending webhooks. |
| `nonces.purge` | 5 min | Forgets alert nonces older than ten minutes. |
| `jobs.purge` | 1 h | Removes finished jobs older than a week. |

Stopping the server waits for the scheduler to finish before closing the database.

## Upgrading

Download the new binary (or image), restart. Migrations run in order at start and are never edited after release. Back up the database first.
