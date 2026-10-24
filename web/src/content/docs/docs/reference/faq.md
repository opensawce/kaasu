---
title: FAQ
description: Short answers to common questions.
---

## Does Kaasu hold my money?

No. The customer's bank pays your bank directly. Kaasu is not a payment gateway and never has your funds.

## Do I need a payment-aggregator licence?

Kaasu does not process payments, so it is not an aggregator. You are still responsible for your own tax and compliance. See [Bank terms](/docs/reference/bank-terms/).

## What if two customers pay the same amount?

They cannot: every open order gets its own exact amount to the paisa. See [Orders and pay links](/docs/use/orders-and-links/).

## What if the customer pays the wrong amount?

The order is held as **Needs your OK**. You see what was expected and what the bank reported, and you approve or reject.

## What if my phone is off or the SMS is late?

The order waits. A second source such as bank email confirms it without the phone. The dashboard warns when you have fewer than two live sources.

## Can someone fake a payment?

A screenshot, a typed UTR or a pasted message cannot confirm anything. Only a signed device alert or a DKIM-verified bank email can, and the exact amount must match. See [Security](/docs/reference/security/).

## Does it work for businesses with many UPI ids?

Yes. Add several receiving accounts. When there is more than one, an API call must say which `account_id` it is for.

## Is AI required?

No. It is off by default and never confirms a payment. See [AI help](/docs/use/ai-features/).

## Which languages?

English and ten Indian languages, with native review still needed. See [Languages and voice](/docs/use/languages-and-voice/).

## What does it cost, and what is the licence?

It is free and open source under the AGPL-3.0. A separate commercial licence is available from OpenSawce for people who cannot meet the AGPL terms. See [Terms](/terms/).

## Which shop platforms work?

WooCommerce works today. [Shopify](/docs/connect/shopify/) is planned. Anything else can use the [API](/docs/reference/api/).
