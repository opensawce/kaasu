---
title: Needs your OK
description: Why Kaasu holds some payments for you, and how to decide them.
---

Kaasu confirms a payment on its own only when everything lines up. Otherwise it **holds the order and asks you**. A wrong "paid" would cost you goods, so it prefers to ask.

## How it decides

Kaasu scores each alert against the order it matches. The points are fixed and public:

| Signal | Points |
|---|---|
| The amount is exactly the order's amount | 40 |
| The alert came from an authentic source (a signed device, or verified bank email) | 20 |
| The alert carries a UTR (the bank's transaction number) | 10 |
| The alert text echoes your order reference | 10 |
| The time fits the order's window | 10 |
| The payer has paid before | 5 |
| Two independent sources reported it | 5 |

It confirms automatically only if the amount is exact, the source is authentic, the score is at least 80 **and** the amount is under that account's auto-confirm limit. A limit of 0 means every payment waits for you.

## Why an order is held

| Reason | What happened |
|---|---|
| `amount_mismatch` | The customer paid a little more or less than asked. |
| `late` | The money arrived after the order expired. |
| `order_closed` | The order was cancelled or rejected before the money arrived. |
| `ambiguous` | Two open orders could both fit the alert. |
| `unauthentic` | The alert was not from a signed device or verified email. |
| `low_score` | Not enough signals agreed. |
| `over_limit` | The amount is above the account's auto-confirm limit. |

## Deciding

Held orders show **Needs your OK** with the evidence: the amount expected next to what the bank said, the score, the reason and the masked payer. The raw alert text is shown escaped, never as live HTML.

- **Approve.** The order becomes confirmed, or late-paid if its window had passed.
- **Reject.** Choose one reason: not received, wrong amount, duplicate, suspected fraud, or other.

Only the **owner** can decide, and only with a fresh sign-in (an authenticator code within the last five minutes). Staff accounts are read-only. Every decision writes the owner's name to the ledger. Deciding twice, or an order with no held alert, is refused instead of changing anything.

## Why this is safe

A person approving a held payment is the one place a human, not the matcher, confirms. It needs the owner role, a valid form token and a fresh code, and it is recorded. See [Security](/docs/reference/security/).
