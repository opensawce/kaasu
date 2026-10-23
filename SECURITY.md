# Security policy

Kaasu confirms payments, so we treat security reports as the highest priority.

## Report a vulnerability

Use **[Report a vulnerability](https://github.com/opensawce/kaasu/security/advisories/new)** on GitHub. It is private. Please do not open a public issue or pull request for a security problem.

Include what you found, how to reproduce it, and what an attacker could do. A redacted bank alert sample is fine; never send real customer data or live keys.

## What to expect

| Step | Target |
|---|---|
| We acknowledge your report | within 3 working days |
| We confirm or explain why it is not a vulnerability | within 10 working days |
| We ship a fix for critical and high issues | within 30 days |
| We publish an advisory and credit you, if you want | with the fix |

## In scope

Anything that could make an order show as paid without a verified bank alert, expose payer or account data, bypass admin sign-in, forge or replay a device alert or webhook, tamper with the ledger without detection, or compromise a release.

## Out of scope

Reports that need physical access to the owner's server, social engineering of maintainers, volume-based denial of service, and findings in third-party services we do not control.

## Safe harbour

If you act in good faith, stay within this policy, and give us a reasonable time to fix, we will not take legal action against you.

## Supported versions

Kaasu is pre-release. Only `main` is supported until the first tagged release.

See the [threat model](architecture.md#5-threat-model) for what we already defend against.
