=== Kaasu for WooCommerce ===
Requires PHP: 8.1
License: AGPL-3.0-or-later

Accept UPI payments through your own self-hosted Kaasu server. Money goes straight from the shopper's bank to yours. Kaasu marks the order paid only after a signed "order.confirmed" webhook arrives.

== Setup ==
1. Set the Kaasu URL, API key and webhook secret in WooCommerce > Settings > Payments > Kaasu.
2. In Kaasu, point the webhook at https://your-store.example/?wc-api=kaasu and use the same secret.
