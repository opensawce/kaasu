---
title: Supported banks
description: Which bank alerts Kaasu understands, and how new ones are added.
---

Kaasu reads **credits** (money received) from these banks. Debits, offers and anything unfamiliar are never treated as a payment.

| Bank | SMS sender ids | Email domain |
|---|---|---|
| HDFC Bank | `HDFCBK` | `hdfcbank.net` |
| State Bank of India | `SBIUPI`, `SBIINB`, `SBIPSG` | `sbi.co.in` |
| ICICI Bank | `ICICIB` | `icicibank.com` |
| Axis Bank | `AXISBK` | `axisbank.com` |
| Kotak Mahindra Bank | `KOTAKB` | `kotak.com` |

Sender ids are matched with their usual operator prefix and suffix, so `VM-HDFCBK-S` counts as `HDFCBK`. A look-alike such as `HDFCBK.evil` does not match.

## What gets read

The amount (in paise, parsed from the digits, never as a decimal number), the 12-digit UTR, the date, a masked payer and the last digits of your account. A reference that is not exactly 12 digits is rejected, so a longer number cannot be truncated into a valid-looking one.

## Not understood?

If a message is not recognised, Kaasu keeps it as evidence and never confirms from it. The optional [AI helper](/docs/use/ai-features/) can propose a reading, which always goes to you for approval.

## Add a bank

Parsers are small and have golden test files. See [Contribute](/docs/reference/contribute/): add a pattern, a **redacted** sample message (no real names, numbers or UTRs) and the expected result.
