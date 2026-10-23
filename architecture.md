# Kaasu architecture

How Kaasu is built: system design, API, security and AI rules. Product, brand and UI live in [`design.md`](design.md). Spec only.

**Contents:** [System](#1-system) · [API & webhooks](#2-api--webhooks) · [Security](#3-security) · [AI](#4-ai) · [Threat model](#5-threat-model)

---

## 1. System

Diagrams are drawn in Eraser. Source: `assets/diagrams/*.eraser` · Images: `assets/diagrams/*.png` · Live file: [Kaasu: System design](https://app.eraser.io/workspace/DGtR2KRhH2uuKnljVGsC).

### Architecture
![Kaasu system architecture](assets/diagrams/architecture.png)

### Payment flow
![Payment confirmation flow](assets/diagrams/payment-flow.png)


**Amount pool rules (issue #7, #8 hardening).** Each order that is not queued holds one exact paise amount from its base to base+99 on its account. A slot is held until `expires_at + 10 min` (grace). Cancelled, rejected and expired orders keep their slot until the grace ends; only a refund frees it at once. A queued order has no payable link: its UPI link appears only when it is promoted, and promotion gives it a fresh window of the same length. `Service.Sweep` promotes queued orders once slots free.

**Evidence and dedupe (issue #15).** Raw alerts are stored byte-for-byte with their SHA-256; the same body twice is one row. A parsed payment is keyed by its UTR; when the UTR is missing it is keyed by a fingerprint of account hint, amount and a 5-minute time bucket. A second source for a known payment adds an `alert_sources` row instead of a new alert. Stored payer data is masked (`ra***@okicici`); the ledger records alert facts but never the raw text.

**Bank alert parsers (issue #16).** One pattern per bank (HDFC, SBI, ICICI, Axis, Kotak) reads credits only; debits, promos and anything unrecognised return `ErrUnknownFormat` and never confirm an order. Amounts are read as integer paise from the digit string (commas stripped, no floats). Times are read in IST and stored as UTC. Sender ids and email domains are allow-listed; a look-alike such as `hdfcbank.net.evil.io` matches nothing. Fixtures are synthetic and checked against golden files.

**Signed ingest (issue #14).** Phones and mail sources reach the server only on the ingest listener (`/ingest/v1/...`), never on public or admin. Pairing uses a single-use token valid 10 minutes, stored as a hash, shown as a `kaasu://pair` link. Every alert request is signed with the device's Ed25519 key over this exact string:
```
KAASU-INGEST-V1\n<deviceID>\n<unixSeconds>\n<nonce>\n<hex(sha256(body))>
```
Headers: `X-Kaasu-Device`, `X-Kaasu-Timestamp`, `X-Kaasu-Nonce` (16 to 64 characters), `X-Kaasu-Signature` (base64). A request is accepted when its timestamp is within 60 seconds of server time, the signature verifies and the device is paired and not revoked. Nonces are stored per device; a repeat is `409 replay`. Every other failure is `401 unauthorized` with no detail. Raw evidence is stored as received, before parsing.

**Matcher and policy (issue #18).** The matcher is the only code that confirms a payment (a test scans the repo to enforce this). An alert is compared with open orders that have the exact amount. One candidate: an order past its expiry is late and goes to review even before the expiry job runs; a cancelled or rejected order goes to review as `order_closed` and stays closed; an awaiting order is scored. The score is built from signals with weights exact 40, authentic 20, UTR 10, note 10, timing 10, payer 5, multiple sources 5. Automatic confirmation needs an exact amount, an authentic source (device or DKIM-verified email, never manual), a score of at least 80, and an account limit the order is under. Anything else goes to review with a reason. Two candidates are ambiguous and both go to review. An alert one to 99 paise above an open order's base goes to review as `amount_mismatch`. Processing the same alert twice changes nothing.

**Email alerts (issue #17).** The owner's inbox is polled over IMAP with an app password read from the vault on each poll. A message counts as an alert only when its DKIM signature verifies on the raw bytes, the signing domain is an allow-listed bank domain (exact or real subdomain), and the From address is aligned with that domain. Anything else is rejected and logged as `email.rejected` with a reason, never with the body. A verified message is stored as email evidence and goes through the same parser and matcher as device alerts. Messages are deduplicated by their raw hash, so a re-polled message changes nothing.

**Scheduler (issue #10).** Time-based work is rows in the `jobs` table, so a restart resumes pending work. Retries wait 10 s, 30 s, 2 min, 10 min, then 1 h, and a job is `dead` after 8 attempts. A job is claimed with a 5-minute lock in a short transaction, runs outside any transaction, and panics are recorded as failures. Recurring jobs are stored in `recurring`. Built-in jobs: `orders.expire` (every 5 s; expires an order exactly at `expires_at`), `pool.sweep` (every minute; promotes queued orders once grace has ended) and `nonces.purge` (every 5 minutes).

**Signed webhooks (issue #11).** Every change that matters to a shop writes an outbox row in the same transaction as the change: `order.confirmed`, `order.review`, `order.rejected`, `order.expired`, `order.cancelled`, `order.refunded`, `order.disputed`. The payload carries the order id, state, amount, reference and account, never payer data. Delivery is at least once with header `X-Kaasu-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, t "." body)>` and `X-Kaasu-Event-Id`. Retries wait 10 s, 30 s, 2 min, 10 min, 1 h, 6 h and 12 h between eight attempts (seven waits), then the event is dead-lettered. Targets must be https and must not resolve to loopback, private or link-local addresses; the guard checks the address actually dialled, and redirects are refused. Receivers verify with `webhooks.Verify` and dedupe by `event_id`.

**Refunds (issue #33).** Kaasu never moves money; a refund is a record of one the merchant made in their own bank app. A refund is allowed only on a confirmed or late-paid order, the total refunded can never exceed the amount paid (checked inside the write transaction, so concurrent refunds cannot overshoot), and a UTR can be recorded once. A partial refund leaves the order in its current state (confirmed or late_paid); the refund that returns the full amount moves it to refunded, which sends the `order.refunded` webhook and frees the slot.

**Reconciliation (issue #32).** A bank statement is imported as CSV (per-bank mappings; generic fallback), with integer paise and a 10 MB and 100,000-row cap. Importing the same file twice stores it once. Reconciliation is read-only: confirmed orders are matched to credits by UTR from their auto-match alert, then by an unused credit of the exact amount (an order with a UTR may only fall back to a credit that carries no UTR). Each imported statement belongs to one receiving account and only matches that account's orders. Four buckets come out: matched, confirmed-but-missing (the owner can move these to disputed), credited-but-unmatched, and amount-mismatch (a UTR match whose credit differs from the confirmed order amount).

**Admin sign-in (issue #12, partial).** Admin sign-in is TOTP (RFC 6238, SHA-1, 30 s, 6 digits, one step of skew, each code works once) with sessions stored only as hashes. Sessions last 12 hours absolute and 30 minutes idle. Five failures lock a name for five minutes; unknown names and wrong codes get the same reply. Sensitive actions need a code within the last five minutes (`RequireFresh`, `Reauth`). Staff are read-only (`RequireOwner`). Forms carry a CSRF token derived from the session. Passkeys are the intended primary method and are not built yet.

**HTTP baseline (issue #13).** Every listener wears the same protections: a strict Content-Security-Policy (`default-src 'self'`, no inline script), `nosniff`, `no-referrer`, `frame-ancestors 'none'`, HSTS only over TLS, a 15-second request limit, a 64 KB body cap, and panic recovery that returns a plain 500 with no stack. Rate limiting is a token bucket per key; client identity ignores forwarded headers unless a trusted proxy is configured.

**Logs and metrics (issue #42).** Every log line passes a redaction handler: VPAs keep two characters of the name, 12-digit UTRs and long account numbers keep their last four digits, API keys and bearer tokens are masked. Requests carry an `X-Request-Id`. `/metrics` (Prometheus text) is served on the admin listener only.

**Simulator and outbound guard (issue #51, partial).** `kaasu sim --list` names the ten payment scenarios with the state each should end in. `sim.Send` signs an alert with a device key and posts it through the real ingest handler, so development runs production code. `sim.CheckOutbound` refuses any call that leaves the machine (localhost is allowed).

**Design tokens (issue #22, partial).** `assets/tokens.json` is the single source for colour, status words, spacing, radius and touch size. `go run ./scripts/gen-tokens` writes `internal/web/static/tokens.css`. Tests check the generated file matches the JSON, the app and the marketing site agree on every shared colour, every status has an icon and a word, icons sit on the 24 px grid, and every status pair meets WCAG AA.

**Languages (issue #28, partial).** The catalogue is `i18n/<lang>.json`, one flat file per language, embedded in the binary. Every language must have every English key with the same placeholders (a test enforces this). `i18n.T` falls back to English. Money renders with Indian grouping and Latin digits (`₹1,24,860.50`). `i18n/REVIEW.md` lists every string that still needs a native-speaker check; hi and ta are machine drafts, and te, kn, ml, mr, bn, gu, pa and or are English until translated.

**Customer pay page (issue #23).** `/pay/{id}` shows the exact amount, one primary button that names the amount and opens the UPI link, the waiting steps, and "Already paid? Don't pay again." Copy comes only from the catalogue. Waiting states never say "failed". `/pay/{id}/status` returns the state alone; `pay.js` polls it every two seconds with no third-party code. The page has no inline script, no third-party request, and stays under 30 KB. Language comes from `?lang=` (kept in a cookie), then the browser, then the shop default.

**Payment confirmed page (issue #24).** `/pay/{id}/done` shows the success screen only when the order is confirmed or late-paid; anything else redirects back to the pay page, and the URL can never make success appear. It shows the amount from the order (a `?amount=` query is ignored), a live "time since confirmed" counter, the UTR, time, reference and a six-character check code. The code is an HMAC of the order, the confirmation time and the amount, so the shopkeeper can compare it. `/pay/{id}/receipt` is the printable receipt (`?format=txt` for plain text). The tick animation respects reduced motion. Paisu never appears on customer pages.

**Payment links and counter QR (issue #25, partial).** A payment link has a fixed amount and an optional expiry; each visit creates or reuses an order through the same orders service as the API, keyed by link, visitor and amount, so a refresh reuses the order. A counter link has no amount: the shop types it (`ParseCounterAmount`: at least ₹1, at most ₹1,00,000, integer paise). Disabled and expired links answer as gone. `WhatsAppURL` builds the share link, and `Standee` is the printable A6 sign that loads nothing from outside.

**Checkout snippet (issue #26, partial).** `kaasu.js` (under 3 KB, no dependencies, no inline handlers) opens the pay page by redirect or in a popup, and reports status from the pay page only when the message comes from that page's origin. `examples/webhooks/vectors.json` holds one signed delivery; the Go receiver example verifies it, and the other languages must do the same.

### Order states
<img src="assets/diagrams/order-states.png" alt="Order state machine" width="420">

### Data model
![Data model](assets/diagrams/data-model.png)

SQLite in WAL mode, STRICT tables, foreign keys on, one writer (`BEGIN IMMEDIATE`). Migrations are numbered files in `internal/store/migrations/`; never edit an applied one.

| Table | Holds | Guarded by |
|---|---|---|
| `receiving_accounts` | UPI IDs the shop receives on | unique VPA |
| `orders` | one row per order, state, exact paise | unique idempotency key, state check |
| `amount_reservations` | the unique amount an order that is not queued holds | primary key (account, amount) |
| `devices`, `nonces` | paired phones and replay protection | unique (device, nonce) |
| `evidence`, `alerts` | raw alert bodies and parsed facts | unique raw hash, unique UTR |
| `matches` | alert to order decisions | one match per alert |
| `refunds` | refunds recorded against orders | foreign key to orders |
| `ledger` | append-only hash chain: `hash = SHA-256(prev_hash, seq, ts, kind, order, actor, payload)`; triggers block UPDATE and DELETE; `kaasu verify` reports the first broken row; the chain detects edited rows but not removal of the newest rows unless the latest hash is kept outside the database (see the backup work) | triggers plus the chain |
| `outbox` | webhook events waiting for delivery | unique event id |

### Deployment
| Option | Diagram |
|---|---|
| Self-host (VPS, Mac, Windows) | Architecture above |
| Railway (one project per shop) | ![Railway](assets/diagrams/deploy-railway.png) |
| Kaasu Cloud on AWS (ap-south-1) | ![AWS](assets/diagrams/deploy-aws.png) |

---

## 2. API & webhooks

- REST under `/v1`, API key per shop, `Idempotency-Key` required on create.
- Money fields are integer paise.
- UPI intent link for personal VPAs: `pa, pn, am, cu, tn` only.
- Webhook header `X-Kaasu-Signature: t=<unix>,v1=<HMAC-SHA256(secret, t.body)>`; unique `event_id`; at-least-once delivery.
- Events: `order.confirmed`, `order.review`, `order.rejected`, `order.expired`, `order.cancelled`, `order.refunded`, `order.disputed`.
- Full spec: `openapi.yaml`.

---

## 3. Security

- Three listeners: public, ingest (signed devices only), admin (localhost/VPN only).
- Device alerts: Ed25519 signature over body hash + timestamp + nonce; replay window 60 s; revocable.
- Email alerts: DKIM verified on raw message; bank domain allowlist.
- Admin: passkeys, TOTP fallback; re-auth for sensitive actions.
- Secrets: envelope-encrypted; master key outside the database; API keys hashed.
- Ledger: append-only, hash-chained, verified by `kaasu verify`.
- Customer pages: strict CSP, no third-party JS.
- Releases: reproducible, signed, SBOM.

---

## 4. AI

- Optional, off by default; merchant's own key or local model.
- Never confirms payments or changes order state.
- Inputs masked; structured output only; no tools.
- Every suggestion stored in the ledger with the model name.
- Features: unknown-format fallback (→ review), statement import, Ask your shop.

---

## 5. Threat model

**Protect:** the truth of "this order is paid", payer and account data, the ledger, admin access, signing keys and releases.
**Trust:** the merchant's own server and the bank's signed alert. **Do not trust:** customers, payer-supplied text (UPI note, UTR, name), forwarded devices until paired, webhook receivers, any LLM output.

| # | Threat | Defence | Test (#57) |
|---|---|---|---|
| T1 | Fake payment: forged screenshot, hand-typed UTR, made-up alert | Only `internal/match` confirms, and only from a verified alert; manual UTR goes to review. The shopkeeper can also compare the six-character check code on the customer's success page or receipt with their own app: it is an HMAC (key kept in settings, stable across restarts) that binds the order, the confirmation time and the amount, so a forged screenshot fails the comparison | Forged and unsigned alerts never confirm |
| T2 | Replayed or edited device alert | Ed25519 over body hash, timestamp and nonce; 60 s window; nonce store | Replay, clock skew, tampered body rejected |
| T3 | Spoofed bank email | DKIM on the raw message; bank domain allow-list | Bad signature and look-alike domain ignored |
| T4 | Wrong order matched (amount collision, late alert) | Unique paise per open order; ambiguous or late matches go to review | Collision and late-alert scenarios |
| T5 | Ledger tampering by an admin or a stolen database | Append-only, hash-chained rows; state change and ledger row in one transaction; `kaasu verify` | Edited row breaks the chain |
| T6 | Admin takeover | Passkeys, TOTP fallback, re-auth for sensitive actions, admin listener not public | Admin routes absent on public and ingest listeners |
| T7 | Secret theft from disk or backup | Envelope encryption; master key outside the database; API keys hashed | Dump contains no plaintext secrets |
| T8 | Payer data leak | Masked VPA and account everywhere except the evidence store; masked logs | Log and response scans for full VPAs |
| T9 | Webhook forgery or replay by a third party | `X-Kaasu-Signature` over `t.body`; unique `event_id`; at-least-once | Bad signature and old timestamp rejected |
| T10 | Prompt injection through alert text | LLM has no tools, never sees raw alert text, cannot change state; output is structured only | Injection corpus changes nothing |
| T11 | Customer page abuse (XSS, clickjacking, third-party script) | Strict CSP, no third-party JS, output escaping | CSP header and injection tests |
| T12 | Poisoned release or dependency | Reproducible signed builds, SBOM, pinned CI actions, Dependabot, CodeQL | Release verifies against its signature |
| T13 | Denial of service on the counter | Rate limits per door, bounded queues, single-writer SQL with short transactions | Load test keeps confirmations flowing |

**Accepted risks:** a compromised merchant server can lie to its own shop; a bank that never sends alerts leaves orders waiting, never paid; the owner's own approval in "Needs your OK" is final and is recorded.

**Review:** update this table in any PR that changes a defence above. An independent audit is tracked in #78.

