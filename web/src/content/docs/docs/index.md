---
title: Kaasu documentation
description: Everything you need to run Kaasu in your shop and help improve it.
---

Kaasu confirms UPI payments for small shops. Customers pay your bank directly, and Kaasu marks the order paid only when a verified bank alert says so.

## Start here

1. [How Kaasu works](/docs/start/how-it-works/): the flow and the rules it never breaks.
2. [Install Kaasu](/docs/start/install/) and run the first-time setup.
3. [Take your first payment](/docs/start/first-payment/).
4. [Connect an alert source](/docs/connect/android-forwarder/) and [your shop](/docs/connect/woocommerce/).

## Everyday use

[Dashboard](/docs/use/dashboard/), [Orders and pay links](/docs/use/orders-and-links/), [Needs your OK](/docs/use/needs-your-ok/), [Refunds](/docs/use/refunds/), [Reconciliation](/docs/use/reconciliation/), [Languages and voice](/docs/use/languages-and-voice/), [AI help](/docs/use/ai-features/), [Backups and keys](/docs/use/backup-and-keys/).

## Reference

[API](/docs/reference/api/), [Webhooks](/docs/reference/webhooks/), [Command line](/docs/reference/cli/), [Configuration](/docs/reference/configuration/), [Orders, states and the ledger](/docs/reference/ledger-and-states/), [Operations](/docs/reference/operations/), [Security](/docs/reference/security/), [Troubleshooting](/docs/reference/troubleshooting/), [FAQ](/docs/reference/faq/).

## What works today

Kaasu is early software. The core is built and tested: orders, exact amounts, the matcher, signed alerts, webhooks, the ledger, the pay and receipt pages, the dashboard and the command line. Phone and Mac apps, the email watcher's mailbox setup, backups and several screens are still being connected, and each page says so where it matters. Test with small amounts first.

## How these docs are kept

Each page is a Markdown file in `web/src/content/docs/docs/`. To fix or add a page, edit the file and open a pull request. The sidebar builds itself from the folders, so a new file appears automatically. See [Contribute](/docs/reference/contribute/).
