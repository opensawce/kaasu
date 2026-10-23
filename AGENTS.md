# AGENTS.md (Kaasu)

Kaasu confirms UPI payments for small Indian shops. Money moves customer bank → merchant bank; Kaasu never touches it. It watches signed bank alerts and marks orders paid. Full spec: `design.md`.

## Commands

| Command | Does |
|---|---|
| `make dev` | Runs everything offline with demo data + simulator |
| `make test` | All tests, offline, < 60 s |
| `make lint` | gofmt, go vet, staticcheck |
| `kaasu sim <scenario>` | Sends a fake signed bank alert (`kaasu sim --list`) |
| `kaasu verify` | Checks the ledger hash chain |

## Map

```
cmd/kaasu/          entrypoint, subcommands
internal/
  orders/ pool/ states/ ledger/      money core
  evidence/ parsers/ match/          alerts → decisions
  webhooks/ scheduler/ recon/        outputs and jobs
  auth/ secrets/                     security
web/                templates (html/template + htmx), static
i18n/               string catalogue, one file per language
apps/android/       Forwarder (Kotlin), own AGENTS.md
apps/apple/         iOS + macOS (Swift), own AGENTS.md
apps/windows/       tray app, own AGENTS.md
assets/             logo, Paisu, icons, tokens.json (SVG is source; WebP generated)
```

## Never

1. Use floats for money. Money is `int64` paise.
2. Confirm a payment anywhere except `internal/match`, and only from a signed, verified alert.
3. Change an order's state outside `internal/states`, or without a ledger row in the same transaction.
4. Send alert text, UPI notes or customer input to an LLM that has tools, or log it unmasked.
5. Show a full payer VPA or account number outside the evidence store.
6. Serve admin routes on the public or ingest listener; load third-party JS on customer pages.
7. Edit an existing migration. Add a new one.
8. Make outbound network calls in tests. Dev mode blocks them; a test that needs the internet is wrong.

## Ask before

- Changing `match/`, `parsers/`, `states/`, `ledger/`, `auth/` or ingest (money path: 2 approvals)
- Adding a dependency, a network call, or a data-rewriting migration
- Changing a webhook payload, public API or UPI link format

## Conventions

- **Go:** stdlib `net/http` and `html/template`; wrap errors with context; no package globals; table-driven tests.
- **SQL:** parameterised only; `BEGIN IMMEDIATE` for reservations; keep transactions short (single writer).
- **UPI links:** personal VPAs use only `pa, pn, am, cu, tn`, never `mc, tr, sign`.
- **UI:** tokens from `assets/tokens.json`; status = icon + colour + word from the fixed set; one primary button per screen.
- **Text:** every string in `i18n/`; no hard-coded copy; amounts with Indian grouping; UPI/UTR/IDs stay Latin.
- **Paisu:** merchant screens only; `happy` only after confirmation.
- **Fixtures:** bank alert samples must be redacted.
- **Diagrams:** edit `assets/diagrams/<name>.eraser`, paste into the Eraser file, re-export `<name>.png` at 2×, all in the same PR. Never hand-edit the PNG.
- **Git:** Conventional Commits; one issue per PR; `Closes #n`.

## AI-assisted coding

Follow the [AI-assisted coding policy](CONTRIBUTING.md#ai-assisted-coding-policy). As an agent you must also never push to `main`, merge PRs, sign the CLA, skip checks, or edit that policy.

## Done means

- Tests + a simulator scenario for any behaviour change
- `make lint test` passes offline
- `design.md` updated if behaviour or rules changed
- UI changes: phone + desktop screenshots, every state, one Indic script checked
