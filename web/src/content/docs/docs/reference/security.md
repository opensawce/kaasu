---
title: Security
description: How Kaasu protects your shop with signed alerts, verified email, safe sign-in and a checkable ledger.
---

- **Three separate doors.** Customer pages, device alerts and the admin screen are served separately. Admin is reachable only from your own machine or VPN.
- **Signed alerts.** Each device signs every alert. Replays and old messages are refused.
- **Verified email.** Email alerts must pass sender verification and come from allowed bank domains.
- **Safe sign-in.** An authenticator code (6 digits, each usable once), lockout after five wrong tries, hashed sessions with idle and absolute limits, form tokens against forged requests, and a fresh code for decisions that move money. Passkeys are planned.
- **Protected secrets.** Keys are encrypted and kept apart from the database.
- **A ledger you can check.** Append only and chained; `kaasu verify` proves it.

Report a vulnerability through the private security advisory form on GitHub, not a public issue.

The full design is in [architecture.md](https://github.com/opensawce/kaasu/blob/main/architecture.md).

## The details

### Who can confirm a payment

Only the matcher, and only from a verified alert whose amount matches exactly, can confirm automatically. A test in the repository scans the code to make sure nothing else changes an order to confirmed. The one human exception is the owner approving a held payment, which needs the owner role, a valid form token and a code entered in the last five minutes, and writes their name to the ledger.

### Alerts

- Device alerts are signed with Ed25519 over a version tag, the device id, the timestamp, a nonce and the body's SHA-256. The window is 60 seconds and each nonce works once.
- Failures return an identical `401` with no detail, so nothing can be learned by probing.
- Bank email must pass DKIM on the raw message, from an allowed bank domain that matches the From address.
- Alert text and UPI notes are untrusted data. They are stored and shown escaped, never executed, and never sent to an AI model unmasked.

### Secrets

Secrets are encrypted with per-secret keys wrapped by the master key. API keys are stored only as hashes. The master key never lives in the database.

### Network

- Customer pages, ingest and admin are three listeners on three ports. Admin routes are mounted only on the admin listener.
- Every response carries a strict content security policy, no third-party script runs on customer pages, and all outbound webhook calls refuse private and loopback addresses and redirects.
- Request bodies, request time and request rate are limited on every listener.

### Supply chain

Releases are built reproducibly and signed with Sigstore; the SBOM is listed in the signed `SHA256SUMS`. GitHub Actions are pinned to full commit SHAs, dependencies are scanned with `govulncheck` and CodeQL runs on every change.

### Verifying a release

```bash
sha256sum --ignore-missing -c SHA256SUMS
cosign verify-blob --certificate kaasu-linux-amd64.pem --signature kaasu-linux-amd64.sig \
  --certificate-identity-regexp 'https://github.com/opensawce/kaasu/.github/workflows/release.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com kaasu-linux-amd64
```

## Known limits

The ledger detects edits but cannot detect the newest rows being removed unless you keep the latest hash elsewhere. The listeners speak plain HTTP and expect TLS from a proxy. An external security audit is planned before 1.0 ([issue #78](https://github.com/opensawce/kaasu/issues/78)).
