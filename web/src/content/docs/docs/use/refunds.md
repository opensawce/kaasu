---
title: Refunds
description: Record a refund against an order.
---

Kaasu does not send money back. Refund from your bank app, then record it so your books and the ledger stay right.

> **Status:** the refund log is built and tested; recording one from a screen or command is still being connected.

## Rules

- A refund can be recorded only against a **confirmed** or **late-paid** order.
- The total refunded can never be more than the amount paid. This is checked inside the same database transaction that writes the refund, so two people recording refunds at once cannot overshoot.
- Each refund can carry the bank's UTR for the money you sent back. A UTR can be recorded once.
- A **partial** refund leaves the order in its current state. The refund that brings the total to the full amount moves the order to **refunded**, which sends an `order.refunded` [webhook](/docs/reference/webhooks/).
- Every refund is written to the ledger.

## Shop plugins

A refund in WooCommerce or Shopify does not move money and does not tell Kaasu. Do both: refund in the bank app, then record it in Kaasu.
