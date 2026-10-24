---
title: Languages and voice
description: Eleven languages, Indian number formats and spoken confirmations.
---

## Languages

Customer pages and merchant screens speak English and ten Indian languages: Hindi, Bengali, Gujarati, Kannada, Malayalam, Marathi, Odia, Punjabi, Tamil and Telugu.

- The pay page picks a language from `?lang=`, then a saved choice, then the browser, then your shop's default.
- Amounts use Indian grouping: ₹1,25,000.50. UPI ids, UTRs and order ids always stay in Latin letters.
- Every string lives in one catalogue file per language. Hindi and Tamil are machine drafts; the rest still show English for many strings. **None has had a native-speaker review yet**, and `i18n/REVIEW.md` lists what needs one. If you read one of these languages, a review is the most useful help you can give.

## Spoken confirmations

The engine can read out "₹499 received" when a payment is confirmed, in your chosen voice language (separate from the screen language) using the device's own text-to-speech.

- It speaks **only** for confirmed or late-paid payments, never for held or unconfirmed ones.
- Each order is announced once, even after a reconnect.
- Whole rupees are read as rupees; paise are added only when there are some.

Voice is wired into the merchant apps as they are built.
