---
title: Orders and pay links
description: How orders get exact amounts, how long they last, and how customers pay.
---

## An order in one minute

An order has an amount, an optional reference and note, an expiry and a state. You create it through the [API](/docs/reference/api/) or a shop plugin. The customer pays from the **pay page** at `/pay/<order id>`.

## Why the amount changes by a few paise

Two customers may owe ₹499 at the same time. If both paid exactly ₹499, Kaasu could not tell whose money arrived. So each open order on an account gets its own amount: ₹499.00, ₹499.01, ₹499.02 and so on, up to 99 paise above the price.

- A customer sees the exact amount and pays it. They may notice the extra paise; the pay page shows it clearly and the UPI link carries it.
- When the customer pays a different amount, Kaasu does not guess. It goes to [Needs your OK](/docs/use/needs-your-ok/).
- Amounts are whole paise everywhere. Decimals are never used for money.

## Slots, expiry and the queue

- An open order **holds its amount** until it expires plus a 10 minute grace period. A cancelled or expired order keeps its amount through that grace, so a late payment cannot be mistaken for someone else's.
- By default an order lasts 15 minutes. You can set 1 minute up to 24 hours.
- If all 100 amounts for a price are taken, a new order is **queued**. It has no pay link yet. When an amount frees up, the oldest queued order is promoted and its expiry window starts fresh from that moment.
- Queued orders on other prices are not blocked by a full price.

## Cancelling

You can cancel an order that has not been paid. A paid, expired or already cancelled order cannot be cancelled.

## Pay links

The pay link is a standard UPI link that opens the customer's app with your UPI ID, your name, the exact amount, INR and your note. For personal UPI IDs it uses only those fields (`pa`, `pn`, `am`, `cu`, `tn`). It never includes merchant codes or signatures.

Reusable **payment links** and a printable **counter QR** for walk-in customers are built in the engine and are being wired to web routes; until then, create orders through the API and show the pay page.

## After payment

The pay page polls its status every two seconds and switches to a success screen for a confirmed or late-paid order. The success screen shows the amount from the order (never from the address bar), the time, your reference, the UTR and a six-character check code. `/pay/<id>/receipt` gives a printable receipt.
