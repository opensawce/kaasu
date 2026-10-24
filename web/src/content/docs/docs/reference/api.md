---
title: API
description: "The Kaasu REST API: authentication, orders, idempotency, errors, limits, customer pages, webhooks and the device ingest API."
---

Kaasu has three HTTP listeners. This page covers the three you may call from code: the **public** order API, the **customer pages**, and the **ingest** API used by bank-alert devices. The admin listener is for people and is never exposed to the internet. The machine-readable spec is `openapi.yaml` in the repository.

## Basics

- Base path `/v1`, JSON in and out, `Content-Type: application/json`.
- Money is an integer number of **paise**: `49907` means ₹499.07. Floats are rejected. Currency is always `INR`.
- Send `Authorization: Bearer <api key>`. Create a key on your server with `kaasu apikey new <label>`; it is shown once and only a hash is stored.
- Every response carries an `X-Request-Id`. Quote it when you report a problem.
- Request bodies are limited to 64 KB, and each listener rate-limits per client address (public: 200 requests a second, burst 400). Over the limit you get `429` with `Retry-After`.

## Create an order

`POST /v1/orders` with an `Idempotency-Key` header (required).

```json
{ "amount_paise": 49907, "reference": "INV-1042", "note": "Order 1042", "expires_in_seconds": 900 }
```

| Field | Meaning |
|---|---|
| `amount_paise` | Required. ₹1 to ₹1,00,000 (`100` to `10000000`). |
| `account_id` | Receiving account. Required only when the shop has more than one active account. |
| `reference` | Your own id, shown to you and on the receipt. |
| `note` | Shown in the customer's UPI app (first 50 characters). |
| `expires_in_seconds` | 60 to 86400, default 900 (15 minutes). |

The reply is `201` with the order. The `amount_paise` you get back is the exact amount the customer must pay: Kaasu adds a few paise (up to 99) so that every open order has a unique amount, and that is how a bank alert is matched to it. `base_amount_paise` is what you asked for.

### Idempotency

Repeat a request with the same key and the same body and you get the first order back with status `200` and the header `Idempotent-Replayed: true`. The same key with a different body is `422 idempotency_conflict`. Use one key per order attempt, for example your cart id.

## Read and cancel

- `GET /v1/orders/{id}` returns the order.
- `POST /v1/orders/{id}/cancel` cancels an unpaid order. An order that is already paid, expired or cancelled returns `409 not_cancellable`.

## The order object

```json
{
  "id": "ord_x7k2m9q4w8r5t1y3u6p0",
  "account_id": "acc_000001",
  "state": "awaiting",
  "base_amount_paise": 49900,
  "amount_paise": 49907,
  "currency": "INR",
  "reference": "INV-1042",
  "upi_link": "upi://pay?pa=shop%40okhdfcbank&pn=Sharma+Sweets&am=499.07&cu=INR&tn=Order+1042",
  "expires_at": "2026-10-24T10:15:00Z",
  "created_at": "2026-10-24T10:00:00Z",
  "updated_at": "2026-10-24T10:00:00Z"
}
```

`upi_link` is present only while the order is `awaiting`. A `queued` order (the amount pool for that price is full) has no link until a slot frees up, so never show a pay button for it.

### States

| State | Meaning |
|---|---|
| `awaiting` | Waiting for the customer to pay. |
| `queued` | Waiting for a free exact amount. It moves to `awaiting` on its own. |
| `confirmed` | A verified bank alert matched the order. Safe to fulfil. |
| `review` | Kaasu is unsure (wrong amount, unknown sender, late). The owner decides. |
| `late_paid` | Paid after the order expired and accepted by the owner. |
| `rejected`, `cancelled`, `expired` | Closed without payment. |
| `refunded`, `disputed` | Set by the owner after the fact. |

Only `confirmed` and `late_paid` mean money arrived. Do not infer payment from anything the customer's browser tells you.

## Errors

Errors are `{"error": {"code": "...", "message": "..."}}`.

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request` | Bad JSON, a float amount, an amount or expiry out of range, a missing key. |
| 401 | `unauthorized` | Missing, invalid or revoked API key. |
| 404 | `not_found` | No such order. |
| 409 | `not_cancellable` | The order can no longer be cancelled. |
| 413 | `too_large` | Body over 64 KB. |
| 422 | `idempotency_conflict` | Same key, different body. |
| 429 | `rate_limited` | Too many requests; wait `Retry-After` seconds. |
| 500 | `internal` | Something went wrong. Details are in the server log under your `X-Request-Id`. |

## Customer pages

Send your customer to `/pay/{order id}` on your public address. These pages need no API key and load no third-party script.

- `GET /pay/{id}` is the pay page: exact amount, one button that opens the UPI app, and live waiting steps.
- `GET /pay/{id}/status` returns `{"state": "..."}` only. The page polls it every two seconds; you can too.
- `GET /pay/{id}/done` shows success only for a confirmed or late-paid order, and redirects to the pay page otherwise.
- `GET /pay/{id}/receipt` is a printable receipt (`?format=txt` for plain text) with the UTR and a six-character check code the shopkeeper can compare with their own app.
- Reusable payment links and a counter QR for walk-in customers exist in the engine but have no web routes yet, so create orders through this API for now.

## Webhooks

Kaasu pushes order changes to your server so you never have to poll. See [Webhooks](/docs/reference/webhooks/) for events, signatures and retries. Ready-made integrations: [WooCommerce](/docs/connect/woocommerce/); [Shopify](/docs/connect/shopify/) is planned.

## Device ingest API

Phones and the Mac agent forward bank alerts to the ingest listener. You only need this to build your own forwarder.

1. **Pair.** `POST /ingest/v1/pair` with the one-time pairing token from the QR code and the device's Ed25519 public key. The token works once and expires after ten minutes.
2. **Send alerts.** `POST /ingest/v1/alerts` with the raw alert JSON (`source`, `sender`, `text`, `received_at`) and four headers: `X-Kaasu-Device`, `X-Kaasu-Timestamp` (unix seconds), `X-Kaasu-Nonce` (16 to 64 characters) and `X-Kaasu-Signature`, the base64 Ed25519 signature over the version, device id, timestamp, nonce and SHA-256 of the body.
3. **Rules.** The timestamp must be within 60 seconds of the server's clock, and a nonce can be used once (a replay gets `409`). Any failed check is the same `401` with no detail. An alert from a sender Kaasu does not know is kept as evidence and never confirms anything.

Alerts from devices are authentic only because they are signed; text that merely looks like a bank message is never trusted.

## Try it without real money

Run `make dev` for demo data and `kaasu sim --list` to see the simulator's scenarios. Dev mode blocks outbound network calls, so you can test your integration offline.
