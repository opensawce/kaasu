---
title: Reconciliation
description: Prove each confirmed payment against your bank statement.
---

Reconciliation compares what Kaasu confirmed with what your bank says it received. It is read-only: it never changes an order by itself.

> **Status:** the import and matching engine is built and tested; the screen and command to run it are still being connected.

## Import a statement

Export a CSV statement from your bank and import it for **one receiving account**. A statement belongs to an account and only checks that account's orders.

- Known layouts exist for common banks, plus a generic layout (`date`, `description`, `credit`, `debit`, `utr`).
- Files up to 10 MB and 100,000 rows. A row with missing columns is an error with its row number, never a crash.
- Importing the same file twice stores it once.
- Amounts are read as integer paise.

## What you get

Each confirmed order in the period lands in one bucket:

- **Matched**: a statement credit with the same UTR (or, if the order has no UTR, the same exact amount) was found.
- **Confirmed but missing**: Kaasu confirmed it but the bank shows no such credit. These deserve a look, and you can move them to **disputed**.
- **Amount mismatch**: a credit with the order's UTR exists but for a different amount.
- **Credited but unmatched**: the bank shows money Kaasu has no order for.

Matching by UTR happens first, so an amount match can never steal a credit that belongs to another order's UTR. An order that has a UTR only falls back to a credit that carries no UTR.

## With AI

An optional helper can propose the column layout for a statement Kaasu does not know. Its answer is always checked against every row of the file before anything is imported. See [AI help](/docs/use/ai-features/).
