# Deploy Kaasu on Railway

One replica only: SQLite has a single writer, so a second copy would split the ledger. Pay and ingest share the service; give each its own domain for TLS.

## Services
1. **Kaasu** from this repository (Dockerfile build, `deploy/railway/railway.json`).
2. **Volume** mounted at `/data` (holds `kaasu.db`).
3. **Bucket** for encrypted backups (see `docs` backups page, card 30). Nothing is uploaded until you configure it.
4. **Tailscale** subnet router, so the admin listener (`127.0.0.1:9000`) is reachable only from your tailnet. Admin is never on a public domain.
5. **Cron** service running `kaasu backup verify` weekly (restore test).

## Variables (sealed)
- `KAASU_MASTER_KEY`: generate with `kaasu keygen` on a trusted machine, paste as a sealed variable. Losing it loses the secrets store.
- `KAASU_PUBLIC_ADDR` `:8080`, `KAASU_INGEST_ADDR` `:8443`, `KAASU_ADMIN_ADDR` `127.0.0.1:9000`.

## Domains
Attach a pay domain (public listener) and an ingest domain (ingest listener). Railway issues TLS.

## Checks after deploy
- `kaasu doctor` from the service shell: every line OK.
- Open the pay domain: a pay page for a test order loads.
- Admin: sign in through the tailnet only.

Status: not published as a Railway template yet. The template editor steps above are the checklist to publish it.
