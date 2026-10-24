---
title: Messages on Mac and iPhone
description: Use an iPhone relayed to a Mac as an alert source (experimental).
---

> **Status:** planned and experimental. The server side (signed ingest) is ready; the Mac agent is not built yet.

## Idea

An iPhone cannot let apps read its SMS. The workaround is to turn on **Text Message Forwarding** to a Mac you own. The Mac's Messages database then holds the bank SMS, and a small Kaasu agent on the Mac reads it and sends each allowed message to Kaasu.

## What it will need

- Full Disk Access for the agent, granted by you.
- Only messages from the bank sender ids you allow are read.
- The agent signs requests with a key held in the macOS Keychain, in exactly the same way as the [Android forwarder](/docs/connect/android-forwarder/).
- It is labelled experimental in the app, and warns you when it stops working, because it depends on how Apple stores messages.

Prefer [bank email](/docs/connect/bank-email/) or an Android phone when you can.
