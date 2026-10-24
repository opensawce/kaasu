---
title: Troubleshooting
description: Common problems and what to check first.
---

Start with `kaasu doctor`: it names the failing check.

## The server will not start

| Message | Fix |
|---|---|
| `master key: ...` | Set `KAASU_MASTER_KEY` to a key from `kaasu keygen`. |
| `listen public on :8080: address already in use` | Another program uses the port. Change `KAASU_PUBLIC_ADDR`. |
| `config: ...` | A typo in `KAASU_CONFIG` or an invalid address. Unknown keys are errors. |
| `serve: unexpected arguments` | `kaasu serve` takes no flags. Use environment variables. |

## An order never confirms

1. Is an alert source live? The dashboard shows a phone as offline after 15 minutes of silence.
2. Did the customer pay the **exact** amount, to the right UPI id? Check the order's `amount_paise`.
3. Did the order expire first? A late payment goes to [Needs your OK](/docs/use/needs-your-ok/).
4. Is the sender allowed? A message from an unknown sender is kept as evidence and never confirms.
5. Is the amount above the account's `--limit-rupees`? Then it waits for you.

## API errors

See the table in [API](/docs/reference/api/#errors). `401` means the key is wrong or revoked; `422` means you reused an `Idempotency-Key` with a different body; `429` means slow down and honour `Retry-After`.

## Webhooks do not arrive

- The address must be `https` and public. `localhost` and private addresses are refused by design.
- Your endpoint must answer `2xx` within 10 seconds.
- Check the signature against the raw body, not re-serialised JSON.
- Events are retried eight times over about a day, then dead-lettered.

## `kaasu verify` fails

The chain shows the first row that does not match. Do not edit the database to fix it. Restore from a backup taken before that row, and report it if you did not change anything yourself.

## Locked out of the dashboard

Five wrong codes lock a person for five minutes. Check your phone's clock is correct, since codes depend on it. If you lost your authenticator, create a new person with `kaasu admin init` from the server.
