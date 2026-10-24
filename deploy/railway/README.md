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
- `PORT` = `8080`. Railway sends its health check (`/healthz`) to `$PORT`, and Kaasu's public listener is `KAASU_PUBLIC_ADDR` `:8080`, so the two must match.
- `KAASU_PUBLIC_ADDR` `:8080`, `KAASU_INGEST_ADDR` `:8443`, `KAASU_ADMIN_ADDR` `127.0.0.1:9000`.

## Admin over your tailnet
The admin listener binds to loopback inside the container, so a tailnet client cannot reach it directly. Pick one:
- **Tailscale Serve** (recommended): run Tailscale in the same service and forward to loopback, for example `tailscale serve --bg --https=443 http://127.0.0.1:9000`. Admin stays on loopback; only your tailnet can open the Serve address.
- **Private interface**: set `KAASU_ADMIN_ADDR` to the container's tailnet address with `KAASU_ADMIN_ALLOW_NON_LOOPBACK=true`, and restrict who can reach it with tailnet ACLs. Never attach a public domain to it.

## Domains
Attach a pay domain (public listener) and an ingest domain (ingest listener). Railway issues TLS.

## Checks after deploy
- `kaasu doctor` from the service shell: every line OK.
- Open the pay domain: a pay page for a test order loads.
- Admin: sign in through the tailnet only.

## Settings in the template editor
`railway.json` carries the build and deploy settings for services created from this repository: Dockerfile build, one replica, start command, `/healthz` check, restart on failure (5 retries). When you publish the template, set the same values in the template editor so services created from the template use them too.

Status: not published as a Railway template yet. The template editor steps above are the checklist to publish it.
