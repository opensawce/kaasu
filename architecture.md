# Kaasu architecture

How Kaasu is built: system design, API, security and AI rules. Product, brand and UI live in [`design.md`](design.md). Spec only.

**Contents:** [System](#1-system) · [API & webhooks](#2-api--webhooks) · [Security](#3-security) · [AI](#4-ai)

---

## 1. System

Diagrams are drawn in Eraser. Source: `assets/diagrams/*.eraser` · Images: `assets/diagrams/*.png` · Live file: [Kaasu: System design](https://app.eraser.io/workspace/DGtR2KRhH2uuKnljVGsC).

### Architecture
![Kaasu system architecture](assets/diagrams/architecture.png)

### Payment flow
![Payment confirmation flow](assets/diagrams/payment-flow.png)

### Order states
<img src="assets/diagrams/order-states.png" alt="Order state machine" width="420">

### Data model
![Data model](assets/diagrams/data-model.png)

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
