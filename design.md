# Kaasu design spec

Source of truth for product, brand, UI and system design. Spec only.

**Contents:** [Product](#1-product) · [Brand](#2-brand) · [UI rules](#3-ui-rules) · [Languages](#4-languages) · [Screens](#5-screens) · [System](#6-system) · [API](#7-api--webhooks) · [Security](#8-security) · [AI](#9-ai)

---

## 1. Product

Self-hosted UPI payment confirmation for small Indian shops. Customers pay any UPI app; money goes straight to the shop's bank; Kaasu confirms the order from the bank's alert.

**Limits**
- Reads bank alerts; not connected to banks. Alerts can be late or missing.
- Built for low-volume shops; not a licensed payment gateway.
- One install = one shop. Kaasu never holds or routes money.

---

## 2. Brand

### Logo
| File | Use |
|---|---|
| `assets/logo/kaasu-coin.svg` | Mark alone |
| `assets/logo/kaasu-app-icon.svg` | Coin on green tile (app icons, favicon) |
| `assets/logo/kaasu-logo-light.svg` / `-dark.svg` | Mark + wordmark |

- Mark: gold coin, inner rim, square hole (cut out, so the background shows through).
- Wordmark: `kaasu`, lowercase, Bricolage Grotesque 800, tracking −0.04em.
- Clear space: ½ coin width on all sides. Minimum: 16 px (favicon variant: larger coin, wider hole).
- Don't: recolour the coin, add sound waves or letters, place on gold.

### Colour
| Token | Hex | Use |
|---|---|---|
| `green` | `#0F7B5F` | Primary, confirmed |
| `mint` | `#3FC59A` | Accents on dark |
| `gold` | `#E8A33D` | Coin, Paisu, highlights |
| `ink` | `#14171A` | Text, dark surfaces |
| `paper` | `#F7F6F2` | Background |
| `amber` | `#9A5410` | Needs your OK |
| `red` | `#B42318` | Problem |
| `grey` | `#5B6168` | Secondary text, waiting |

### Type
| Role | Font |
|---|---|
| Amounts, headings | Bricolage Grotesque 700/800 |
| UI text | IBM Plex Sans 400/500/600 |
| UTRs, codes | IBM Plex Mono |
| Indic scripts | Noto Sans (per script) |

### Paisu
| Mood | Shown when |
|---|---|
| `happy` | Payment confirmed (only then) |
| `waiting` | Order open, no alert yet |
| `ask` | Needs your OK |
| `sleepy` | Alert source offline |
| `worried` | Error, reconciliation mismatch |

Merchant screens only. Empty states and onboarding; not in tables or the review queue. Assets: `assets/mascot/paisu-{mood}.svg` (+ WebP 1024/512/256).

---

## 3. UI rules

**Money**
- Amount is the largest text on any screen that has one. `₹` + two decimals; tabular figures in lists.
- Paise highlighted when they identify the order (₹499**.07**).
- Payer VPAs masked (`pri•••@upi`); never full account numbers.

**Status**
- Icon + colour + word, always. Fixed words: *Money received* · *Needs your OK* · *Waiting* · *Phone offline*.

**Waiting**
- Steps with time estimate, never a bare spinner.
- Never "failed" while unknown; show *Waiting* + *Already paid? Don't pay again.*
- Success screens include a live element (ticking clock).

**Actions**
- Buttons name the outcome and amount: *Pay ₹499.07 with any UPI app*.
- One primary button per screen. Destructive = red outline, never primary.
- Passkey re-auth for confirming above the limit; one tap otherwise.

**Errors**
- Say what happened, whether money moved, what to do next.
- After two failures, offer help (call the shop), not retry.

**Layout & motion**
- Phone first; 44 px touch targets; cards for groups, lists for records; no nested cards.
- Alert-source health always visible on merchant home.
- Animate state changes only, < 300 ms; respect reduced motion.

**Accessibility**
- WCAG 2.1 AA: 4.5:1 text contrast, labelled inputs, visible focus.

---

## 4. Languages

English, हिन्दी, தமிழ், తెలుగు, ಕನ್ನಡ, മലയാളം, मराठी, বাংলা, ગુજરાતી, ਪੰਜਾਬੀ, ଓଡ଼ିଆ.

- Globe button in every header opens the shared picker (native name + English).
- Customer pages: browser language, else shop default. Merchant apps: app language and voice language set separately.
- Digits 0–9; Indian grouping (₹1,24,860.50). UPI, UTR, IDs stay Latin.
- Noto Sans per script; line-height ≥ 1.5; no negative tracking on Indic text; layouts tolerate +40% text.
- Strings in `i18n/`; native-speaker review before release.

---

## 5. Screens

Images in `assets/screens/`.

| Screen | Platform | Purpose |
|---|---|---|
| Landing | Web | Explain, install, pricing, honest limits |
| Pay page | Web (phone) | Customer pays via UPI intent or QR; waiting timeline |
| Payment confirmed | Web (phone) | Live receipt that a screenshot can't fake |
| Home | iPhone | Today's total, sources health, latest payments |
| Review | iPhone | Confirm or reject a held payment with evidence |
| Forwarder | Android | Forward bank SMS/notifications; voice language |
| Dashboard + menu bar | Mac | Orders, sources, menu-bar status |
| Review queue | Windows | Expected vs bank-said, confirm with Windows Hello |
| Languages | All | Picker, scripts, voice phrases |

---

## 6. System

Diagrams are drawn in Eraser. Source: `assets/diagrams/*.eraser` · Images: `assets/diagrams/*.png` · Live file: [Kaasu: System design](https://app.eraser.io/workspace/DGtR2KRhH2uuKnljVGsC).

### Architecture
![Kaasu system architecture](assets/diagrams/architecture.png)

### Payment flow
![Payment confirmation flow](assets/diagrams/payment-flow.png)

### Order states
<img src="assets/diagrams/order-states.png" alt="Order state machine" width="420">

### Data model
![Data model](assets/diagrams/data-model.png)

### Deployment
| Option | Diagram |
|---|---|
| Self-host (VPS, Mac, Windows) | Architecture above |
| Railway (one project per shop) | ![Railway](assets/diagrams/deploy-railway.png) |
| Kaasu Cloud on AWS (ap-south-1) | ![AWS](assets/diagrams/deploy-aws.png) |

---

## 7. API & webhooks

- REST under `/v1`, API key per shop, `Idempotency-Key` required on create.
- Money fields are integer paise.
- UPI intent link for personal VPAs: `pa, pn, am, cu, tn` only.
- Webhook header `X-Kaasu-Signature: t=<unix>,v1=<HMAC-SHA256(secret, t.body)>`; unique `event_id`; at-least-once delivery.
- Events: `order.confirmed`, `order.review`, `order.rejected`, `order.expired`, `order.cancelled`, `order.refunded`, `order.disputed`.
- Full spec: `openapi.yaml`.

---

## 8. Security

- Three listeners: public, ingest (signed devices only), admin (localhost/VPN only).
- Device alerts: Ed25519 signature over body hash + timestamp + nonce; replay window 60 s; revocable.
- Email alerts: DKIM verified on raw message; bank domain allowlist.
- Admin: passkeys, TOTP fallback; re-auth for sensitive actions.
- Secrets: envelope-encrypted; master key outside the database; API keys hashed.
- Ledger: append-only, hash-chained, verified by `kaasu verify`.
- Customer pages: strict CSP, no third-party JS.
- Releases: reproducible, signed, SBOM.

---

## 9. AI

- Optional, off by default; merchant's own key or local model.
- Never confirms payments or changes order state.
- Inputs masked; structured output only; no tools.
- Every suggestion stored in the ledger with the model name.
- Features: unknown-format fallback (→ review), statement import, Ask your shop.
