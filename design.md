# Kaasu design spec

Source of truth for product, brand and UI. System design lives in [`architecture.md`](architecture.md). Spec only.

**Contents:** [Product](#1-product) · [Brand](#2-brand) · [UI rules](#3-ui-rules) · [Languages](#4-languages) · [Screens](#5-screens)

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
