---
title: AI help (optional)
description: What the optional AI features are, what they never do, and how they are limited.
---

AI is **off by default** and Kaasu works completely without it. Turning it on is your choice, with your own provider key.

## What it can do

| Feature | What it does | What it never does |
|---|---|---|
| Unknown alert formats | Reads a bank message no built-in parser knows and proposes the fields. | Confirm a payment. The result goes to review marked as AI-read. |
| Statement mapping | Suggests which column is date, credit, debit and UTR in a statement. | Import anything. The suggestion is checked against every row first. |
| Ask your shop | Picks one of four fixed read-only questions (totals, order counts, refunds, held payments) and shows the numbers it used. | Run free-form queries, or write anything. |
| Hints and summaries | Writes plain-language hints and review summaries from facts Kaasu already computed. | Decide, approve or change an order. |

## The guard rails

- **No tools.** The model cannot call functions or touch your data. It receives masked text and returns one structured answer.
- **Masked first.** Account numbers, UPI addresses, email addresses and UTRs are replaced before any text leaves your machine. If something still looks unmasked, the call is refused.
- **Checked output.** The answer must match a strict schema, with no extra fields and no trailing text, or it is discarded.
- **Never authentic.** An AI-read alert can never be the source of an automatic confirmation.
- **Budget.** A monthly cap in rupees (on the India calendar month) stops calls when reached. A cap of 0 means no calls.
- **Consent and record.** You must acknowledge the data notice before use, each feature has its own switch, and every answer is logged with provider and model, without the text.

## Status

The provider layer and each feature are implemented and tested with a stub provider. Choosing a real provider and entering a key from a settings screen is not available yet, so nothing is sent to any AI service today.
