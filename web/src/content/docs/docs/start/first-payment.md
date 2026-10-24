---
title: Your first payment
description: Create an order, open its pay page and see how a payment is confirmed.
sidebar:
  order: 2
---

This walk-through uses a ₹1 order so you can test with real money safely.

## 1. Create the order

```bash
curl -s -X POST http://localhost:8080/v1/orders \
  -H "Authorization: Bearer $KAASU_KEY" \
  -H "Idempotency-Key: first-test-1" \
  -H "Content-Type: application/json" \
  -d '{"amount_paise": 100, "reference": "test-1"}'
```

The reply has the order `id`, its `state` (`awaiting`) and the **exact** `amount_paise` the customer must pay. It may be a few paise above what you asked for, for example `103`. That small difference is how Kaasu tells two customers apart.

## 2. Open the pay page

Send the customer to `http://localhost:8080/pay/<order id>` (use your public address in real life). The page shows the amount and one button that opens their UPI app with everything filled in. It also shows three waiting steps and never says "failed" while the result is unknown.

## 3. Pay it

Pay from any UPI app, exactly the amount shown.

## 4. See it confirm

Your bank sends an alert. A paired phone or the bank-email watcher forwards it, Kaasu checks the signature, reads the amount, finds the one order with that exact amount and marks it **confirmed**. The pay page switches to the success screen, and your webhook fires if you set one up.

To read the order at any time:

```bash
curl -s -H "Authorization: Bearer $KAASU_KEY" http://localhost:8080/v1/orders/<order id>
```

## What each state means

Every order shows an icon, a colour and a word. Colour is never the only signal.

| Word | Meaning |
|---|---|
| **Waiting** | The customer has not paid yet. |
| **Money received** | Confirmed from a verified alert. |
| **Needs your OK** | Kaasu is unsure. [You decide](/docs/use/needs-your-ok/). |
| **Expired** or **Rejected** | Closed without payment. |

## Try it without real money

`kaasu sim --list` shows the payment scenarios the simulator knows: an exact match, a one-paisa edit, a late alert, the same payment by SMS and email, a replayed request, a forged sender, an unknown format, a payment that never arrives, a full pool and a refund. Running a scenario against a live server is still being built; the same scenarios run in the automated tests today.
