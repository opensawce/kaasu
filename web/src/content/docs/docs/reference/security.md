---
title: Security
description: How Kaasu protects your shop with signed alerts, verified email, safe sign-in and a checkable ledger.
---

- **Three separate doors.** Customer pages, device alerts and the admin screen are served separately. Admin is reachable only from your own machine or VPN.
- **Signed alerts.** Each device signs every alert. Replays and old messages are refused.
- **Verified email.** Email alerts must pass sender verification and come from allowed bank domains.
- **Safe sign-in.** Passkeys, with an authenticator code as backup.
- **Protected secrets.** Keys are encrypted and kept apart from the database.
- **A ledger you can check.** Append only and chained; `kaasu verify` proves it.

Report a vulnerability through the private security advisory form on GitHub, not a public issue.

The full design is in [architecture.md](https://github.com/opensawce/kaasu/blob/main/architecture.md).
