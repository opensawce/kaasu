---
title: How Kaasu works
description: The whole flow in one page, and what Kaasu will never do.
sidebar:
  order: 3
---

Money moves from your customer's bank straight to yours. Kaasu never touches it. It only **watches for the bank's own message** that the money arrived, and marks the order paid.

## The flow

1. Your shop creates an order through the [API](/docs/reference/api/) (or a plugin does).
2. Kaasu gives it an **exact amount** in paise that no other open order has. This is the key to matching.
3. The customer pays that amount with any UPI app.
4. Your bank sends an alert. A signed device or a verified bank email hands it to Kaasu.
5. The **matcher** compares the alert with open orders. One order with that exact amount, an authentic source, a plausible time and a UTR: it confirms. Anything less certain goes to you.
6. Kaasu writes the result to the [ledger](/docs/reference/ledger-and-states/), updates the pay page and sends your [webhook](/docs/reference/webhooks/).

## Rules that are never broken

- Money is a whole number of paise. No floats, anywhere.
- Only the matcher confirms a payment automatically, and only from a verified alert. Text typed by a customer, a screenshot or an AI answer never confirms anything.
- An owner can confirm a held payment, with their name written to the ledger, after signing in with a fresh authenticator code.
- Every state change writes a ledger row in the same database transaction.
- Customer pages, device alerts and the admin screen are served on separate ports, so a mistake on one cannot open the others.

## What it needs from you

At least one **alert source**: a paired phone, or bank emails that pass verification. Two independent sources are better: the dashboard warns when only one is live. See the [Connect](/docs/connect/android-forwarder/) section.

## Where it stores things

One SQLite file (`kaasu.db`), encrypted secrets inside it, and nothing sent anywhere unless you turn on an optional feature such as [AI help](/docs/use/ai-features/).
