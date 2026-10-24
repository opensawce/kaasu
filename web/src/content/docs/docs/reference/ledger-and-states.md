---
title: Orders, states and the ledger
description: The order state machine and the tamper-evident ledger behind it.
---

## States

An order moves along fixed edges only. Nothing outside the state machine changes a state, and every move writes a ledger row in the same transaction.

| From | Can go to |
|---|---|
| `created` | `awaiting`, `queued`, `cancelled` |
| `queued` | `awaiting`, `cancelled`, `expired` |
| `awaiting` | `matched`, `review`, `cancelled`, `expired` |
| `matched` | `confirmed`, `review` |
| `review` | `confirmed`, `late_paid`, `rejected`, `awaiting` (only before the order expires) |
| `expired` | `review` |
| `confirmed`, `late_paid` | `refunded`, `disputed` |
| `disputed` | `confirmed`, `refunded` |
| `rejected`, `cancelled`, `refunded` | none |

An alert that arrives for an expired order puts it into `review`; Kaasu never confirms a late payment on its own.

## Amount slots

Each open order holds one exact amount on its account. The hold lasts until the order's expiry plus 10 minutes. A cancelled, rejected or expired order keeps its slot through that grace window, and only a refund frees it at once. See [Orders and pay links](/docs/use/orders-and-links/).

## The ledger

The ledger is an append-only table. Each row stores a SHA-256 hash of its own content and the previous row's hash, so the rows form a chain. Database triggers block `UPDATE` and `DELETE`.

- The row records the kind (for example `order.confirmed`), the order, the actor (`api:<key>`, `matcher`, `admin:<name>`, `cli`) and a small payload.
- It never stores raw alert text or full payer identifiers.
- `kaasu verify` recomputes the chain. Editing, removing or re-ordering a row breaks it from that row on.

The chain proves nothing was changed in the middle. Removing the newest rows is only detectable if you keep the latest hash somewhere else, which is why off-site backups matter.

## Evidence

The raw alert bytes are stored once, exactly as received, in an evidence store. The same payment arriving by two sources becomes one alert with two sources, which raises the match score. A payer address is masked everywhere except in this evidence store.
