---
title: Android forwarder
description: Pass bank SMS and app alerts from an Android phone to Kaasu.
sidebar:
  order: 1
---

> **Status:** the server side (pairing and signed ingest) is done and tested. The Android app itself is not built yet; this page describes what it will do and how its protocol works, so you can build against it.

## What it will do

A small app on the phone that receives your bank's SMS and notifications. It forwards only messages from the bank sender ids you allow, signs each with a key that never leaves the phone, and sends them to Kaasu. It keeps a short list of what it forwarded so you can see it working, and a "send test" button.

## Pairing

1. Kaasu shows a one-time pairing code as a QR (valid ten minutes, usable once). It contains your ingest address.
2. The phone creates an Ed25519 key pair in the Android Keystore and sends the public key with the code.
3. Kaasu stores the public key against a device id. The private key never leaves the phone.

## Sending alerts

Each alert is a JSON body (`source`, `sender`, `text`, `received_at`) with four headers: device id, timestamp, a 16 to 64 character nonce, and the signature over all of it. Details in [API: device ingest](/docs/reference/api/).

- An alert whose timestamp is more than 60 seconds off is refused.
- A nonce can be used once, so a captured request cannot be replayed (the second try gets `409`).
- Every other failure is the same `401` with no detail.
- A revoked phone is refused at once.

## Battery savers

Phones kill background apps to save power. The app will include per-brand steps (Xiaomi, Oppo, Vivo, Samsung and others) and a foreground notification, because a phone that silently stops forwarding is the main way a shop loses confirmations. The dashboard shows **Phone offline** when it has not heard from the phone for 15 minutes.

## Until it ships

Use [bank email](/docs/connect/bank-email/) as a source, or build a forwarder to the protocol above.
