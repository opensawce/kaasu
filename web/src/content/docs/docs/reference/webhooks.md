---
title: Webhooks
description: Events Kaasu sends to your systems.
---

Kaasu sends a signed `POST` for each event. Delivery is at least once, so use `event_id` to ignore repeats.

## Events

`order.confirmed`, `order.review`, `order.rejected`, `order.expired`, `order.cancelled`, `order.refunded`, `order.disputed`

## Verify the signature

The header looks like `X-Kaasu-Signature: t=<unix>,v1=<hmac>`. Compute HMAC-SHA256 of `t.body` with your webhook secret and compare it to `v1`. Reject old timestamps.
