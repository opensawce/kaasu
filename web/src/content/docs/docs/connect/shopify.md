---
title: Shopify
description: Plan for taking UPI payments in a Shopify store through Kaasu.
---

> **Status: planned.** Shopify support is tracked in [issue #138](https://github.com/opensawce/kaasu/issues/138). Nothing on this page works yet; it describes what we are building so you can comment on it.

## How it will work

1. The customer checks out in Shopify and chooses a manual payment method named "UPI (Kaasu)".
2. Shopify's `orders/create` webhook reaches Kaasu, which creates an order for the exact amount and shows the pay link on the thank-you page.
3. The customer pays your bank directly. Kaasu confirms from the verified bank alert.
4. Kaasu calls Shopify's Admin API to mark the order paid. The browser never decides this.

## What to expect

- INR stores only. Amounts are whole paise end to end.
- Shopify webhooks are checked with the app secret, and Kaasu webhooks with your webhook secret. Both are safe to retry.
- Refunds stay a log in Kaasu; you refund in Shopify and in your bank app.
- A payments-app listing in the Shopify store may come later, once the manual-method version is proven.

Want it sooner or have a store to test with? Comment on the issue.
