---
title: Webhooks
description: Events Kaasu sends to your systems, how to verify them and how retries work.
---

Kaasu sends a signed `POST` to your server whenever an order changes in a way that matters. Delivery is **at least once**, so the same event can arrive more than once: dedupe by `event_id`.

Each event is written to an outbox **in the same database transaction** as the change that caused it, so a crash never loses an event or sends one for a change that was rolled back.

## Events

| `type` | Sent when |
|---|---|
| `order.confirmed` | A verified alert matched the order (or the owner approved it). Safe to fulfil. |
| `order.review` | The order is held for the owner. |
| `order.rejected` | The owner rejected a held payment. |
| `order.expired` | The order expired unpaid. |
| `order.cancelled` | The order was cancelled. |
| `order.refunded` | The full amount was refunded. |
| `order.disputed` | Reconciliation or the owner marked it disputed. |

Act only on final states. A payment that arrived after expiry and that the owner accepted is also sent as `order.confirmed`, with `state` set to `late_paid` in the payload, so check `state` if you treat late payments differently.

## The request

```http
POST /your/webhook HTTP/1.1
Content-Type: application/json
X-Kaasu-Event-Id: evt_5a1c...
X-Kaasu-Signature: t=1767261600,v1=3d73b6f9...

{"event_id":"evt_5a1c...","type":"order.confirmed","created_at":"2026-10-24T10:02:11Z",
 "data":{"order":{"id":"ord_...","state":"confirmed","amount_paise":49907,
 "reference":"INV-1042","account_id":"acc_000001","confirmed_at":"2026-10-24T10:02:10Z"}}}
```

The payload carries ids, state, amount and your reference. It never carries payer names, UPI addresses or account numbers.

## Verify the signature

`X-Kaasu-Signature` is `t=<unix seconds>,v1=<hex>`. Compute HMAC-SHA256 with your webhook secret over the string `t`, a dot, then the **raw request body** (exactly the bytes received, before any JSON parsing). Compare with `v1` in constant time, and reject timestamps more than five minutes from your clock.

```python
import hmac, hashlib, time

def verify(secret: str, header: str, body: bytes, tolerance=300) -> bool:
    parts = dict(p.split("=", 1) for p in header.split(","))
    ts, v1 = int(parts["t"]), parts["v1"]
    if abs(time.time() - ts) > tolerance:
        return False
    want = hmac.new(secret.encode(), f"{ts}.".encode() + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, v1)
```

```js
import { createHmac, timingSafeEqual } from 'node:crypto';

export function verify(secret, header, body, tolerance = 300) {
  const p = Object.fromEntries(header.split(',').map((x) => x.split('=')));
  if (Math.abs(Date.now() / 1000 - Number(p.t)) > tolerance) return false;
  const want = createHmac('sha256', secret).update(`${p.t}.`).update(body).digest('hex');
  return want.length === p.v1.length && timingSafeEqual(Buffer.from(want), Buffer.from(p.v1));
}
```

```php
function kaasu_verify(string $secret, string $header, string $body, int $tol = 300): bool {
    parse_str(str_replace(',', '&', $header), $p);
    if (abs(time() - (int) $p['t']) > $tol) return false;
    return hash_equals(hash_hmac('sha256', $p['t'] . '.' . $body, $secret), $p['v1']);
}
```

The repository's `examples/webhooks/vectors.json` has a known secret, body and header. Every language example is tested against it, so use it to check your own code.

## Respond and retry

Answer with any `2xx` within 10 seconds. Anything else, or a timeout, is retried with growing waits: 10 seconds, 30 seconds, 2 minutes, 10 minutes, 1 hour, 6 hours and 12 hours, eight attempts in all. After the last one the event is **dead-lettered**: it is kept, shown to you as a problem, and can be sent again by hand.

## Where Kaasu will send

- The address must be `https`.
- It must not be `localhost` or resolve to a private, loopback or link-local address. Kaasu checks the address it actually connects to, so a DNS trick cannot get past the check, and it never follows redirects.
- The signing secret is stored encrypted.

## Setting it up

The webhook address is the `webhook.url` setting and the secret is stored under `webhook_secret`. A settings screen and a command for these are being connected; until then they are set directly in the database by an administrator. Shop plugins such as [WooCommerce](/docs/connect/woocommerce/) document their own fields.
