---
title: WooCommerce
description: Take UPI payments in a WooCommerce shop through your own Kaasu server.
---

The **Kaasu for WooCommerce** plugin adds a payment method that sends the customer to a Kaasu pay page. WooCommerce marks the order paid only when Kaasu sends a signed `order.confirmed` webhook, never because the customer's browser says so.

## Set up

1. Copy `integrations/woocommerce/kaasu-for-woocommerce` from the repository into `wp-content/plugins/` and activate it.
2. In **WooCommerce > Settings > Payments > Kaasu**, enter your Kaasu URL, an API key (`kaasu apikey new woo`) and a webhook secret.
3. In Kaasu, point the webhook at `https://your-store.example/?wc-api=kaasu` and use the same secret. See [Webhooks](/docs/reference/webhooks/).
4. Place a ₹1 test order and pay it with any UPI app.

## Good to know

- Amounts are sent in whole paise, so ₹499.07 stays exactly that. Only INR shops are supported.
- Each webhook is verified with its signature and de-duplicated by event id, so a retry never marks an order twice.
- A refund in WooCommerce does not move money. Refund in your bank app, then record it in Kaasu. See [Refunds](/docs/use/refunds/).
