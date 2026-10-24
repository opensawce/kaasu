---
title: Install
description: Run Kaasu on your own machine or server, step by step.
sidebar:
  order: 1
---

Kaasu is one program with one database file. You need something that stays on during shop hours: a small cloud server, a spare PC or a Mac. Nothing else has to be installed.

## Choose how to run it

| Way | Best for | Notes |
|---|---|---|
| **Installer script** | A Linux server with systemd | Downloads the release, checks its SHA-256, creates a locked-down service. |
| **Docker or Compose** | Any machine with Docker | Runs as a non-root user with a persistent volume. |
| **From source** | Trying it, or contributing | Needs Go. `make dev` runs the server. |
| **Railway** | One-click hosting | Template configuration is in the repository; the public template is not published yet. |

### Linux installer

```bash
curl -fsSL https://raw.githubusercontent.com/opensawce/kaasu/main/install.sh -o install.sh
sh install.sh --dry-run      # prints what it would do and changes nothing
sudo sh install.sh
```

The script is safe to run again. It downloads the binary for your machine, verifies it against `SHA256SUMS`, creates a `kaasu` user, writes a master key to `/etc/kaasu/master.key` (readable by root only) and starts the `kaasu` service. Check the release signature too: see [Security](/docs/reference/security/).

### Docker

```bash
git clone https://github.com/opensawce/kaasu && cd kaasu
mkdir secrets && go run ./cmd/kaasu keygen > secrets/master.key
sudo chown 65532 secrets/master.key && sudo chmod 0400 secrets/master.key
docker compose up -d
```

The container runs as user 65532, stores data in the `kaasu-data` volume at `/data`, and reads the master key from the Compose secret. The admin listener stays on `127.0.0.1` inside the container; see [Configuration](/docs/reference/configuration/) before exposing anything.

### From source

```bash
git clone https://github.com/opensawce/kaasu && cd kaasu
export KAASU_MASTER_KEY=$(go run ./cmd/kaasu keygen)
make dev
```

## First-time setup

Run these once, as the same user and with the same `KAASU_MASTER_KEY` and `KAASU_DATABASE_PATH` the server uses.

```bash
kaasu account add --vpa shop@okhdfcbank --bank HDFC --name "Sharma Sweets" --limit-rupees 5000
kaasu apikey new website          # prints a key once; store it in your shop software
kaasu admin init --name priya     # prints an authenticator link once; scan it with your authenticator app
kaasu doctor                      # every line should say OK
```

`--limit-rupees` is the most Kaasu may confirm on its own for this account. Anything above it, or anything it is unsure about, waits for you. See [Needs your OK](/docs/use/needs-your-ok/).

## Keep the master key safe

The master key encrypts every secret Kaasu stores (API key hashes, the TOTP secrets, the webhook secret). Lose it and those secrets cannot be read; leak it and they can. Keep a copy offline. Rotate it with `kaasu rotate-key`: see [Command line](/docs/reference/cli/).

## Check the ledger

```bash
kaasu verify
```

A healthy result reads "ledger intact". Any edit to history makes it fail and names the first broken row.
