---
title: API
description: REST API basics.
---

- Base path `/v1`.
- One API key per shop.
- `Idempotency-Key` header is required when creating an order.
- Money fields are integer paise, for example `49907` means ₹499.07.

The full specification lives in `openapi.yaml` in the repository.
