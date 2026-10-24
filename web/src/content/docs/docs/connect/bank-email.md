---
title: Bank email
description: Use bank email alerts as a second, independent source.
---

> **Status:** the email checker is built and tested. It is not started by the server yet, and there is no setting screen for the mailbox, so it does not run in a normal install today.

## How it works

Kaasu reads an inbox that receives your bank's alert emails over IMAP with TLS, using an app password. For every unread message it:

1. Checks the **DKIM signature** on the raw message.
2. Requires the signing domain to be on the bank allow list (for example `hdfcbank.net`, `icicibank.com`, `axisbank.com`, `sbi.co.in`, `kotak.com`) **and** to match the From address.
3. Reads the body, decoding base64 and quoted-printable and looking inside nested parts, with a size limit.
4. Parses the credit with the bank's parser. Unknown formats are kept as evidence and never confirm anything.
5. Marks the message read so it is not fetched again.

A message that fails any step is logged as rejected with a reason (`no_dkim`, `bad_dkim`, `domain_not_allowed`, `from_not_aligned`) and ignored. A copy that arrives by SMS and by email is recognised as the **same payment**, which adds a second source and raises confidence.

## Tips

- Use a dedicated mailbox and an app password, never your main password.
- Forward only bank alerts to it.
- Email alone cannot be hurried by an attacker: a forged email fails DKIM, and an old one fails the time window.
