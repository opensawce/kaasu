---
title: The dashboard
description: Sign in, see today's money and check that your alert sources are alive.
---

The dashboard is served on the **admin** port, which listens on your own machine by default (`127.0.0.1:9000`). Reach it from a VPN or SSH tunnel; never put it on a public address.

## Sign in

You sign in with your name and a 6-digit code from an authenticator app. Create the account once with `kaasu admin init --name <name>` and scan the link it prints. The code works once, so a code someone saw cannot be replayed.

- Five wrong codes lock the account for five minutes. An unknown name and a wrong code give the same reply.
- A session lasts up to 12 hours and ends after 30 idle minutes. The session cookie is stored hashed on the server.
- Decisions that move money (approving a held payment) ask for a fresh code if you signed in more than five minutes ago.
- Two roles: **owner** can decide payments; **staff** can look but not change.

## Today

Shows money received today (India time), the number of payments, and a list of your alert sources with a health word. A phone not heard from in 15 minutes shows **Phone offline**. With fewer than two live sources the page warns you, because one source is a single point of failure. With none, it says nothing is being seen.

## Orders

The 50 most recent orders with their status word and the masked payer (for example `ra***@okicici`). A full payer address is never shown here.

## Monitoring

`/metrics` on the admin port serves Prometheus metrics. Logs are JSON lines with request ids, and account numbers, UTRs and payer addresses are masked before they are written. See [Operations](/docs/reference/operations/).

## Coming to the dashboard

A review-queue screen, device pairing and revocation, settings (webhook address, accounts) and a first-run setup wizard with the ₹1 test payment are built as parts of the engine and are being connected to screens. Until then use the command line: [Command line](/docs/reference/cli/).
