# Privacy

> **Draft: has not been reviewed by a lawyer.** This describes what the software does, not legal advice.

## Who is responsible
- **Self-hosted:** you run Kaasu on your own machine. You decide what it stores and you are the data fiduciary under India's Digital Personal Data Protection Act, 2023 (DPDP). OpenSawce receives none of your data.
- **Managed hosting (planned):** OpenSawce acts as a processor for you. Processor terms, sub-processors and data location will be published before managed hosting launches.

## What Kaasu stores
- Orders: amount in paise, reference you set, state, timestamps.
- Payer identifiers: masked (for example `ra***@okicici`). Full VPAs stay only in the evidence store.
- Raw bank alerts (evidence): the text as received, so a decision can be checked later.
- The ledger: hash-chained event records, which never hold raw alert text.
- Admin sign-in data: a hashed session id; the TOTP secret is encrypted.

## Retention
You choose how long evidence is kept. Ledger rows are kept so the chain stays verifiable.

## Your rights
You can see, export and delete the data your instance holds. Deleting ledger rows is not possible by design; the chain would break.

## AI features (off by default)
AI provider integration is planned and is not part of this version: nothing is sent to any AI provider today. When it ships, it will be off by default, and account numbers and VPAs will be replaced (masked) before any text is sent. The provider's own terms will apply to what it receives.

## Children
Kaasu is for businesses and is not directed at children.

## Breaches
Report security problems as described in SECURITY.md.

## Contact
Open an issue, or use a private security advisory for anything sensitive.
