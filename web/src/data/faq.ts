// Single source for the visible FAQ and its FAQPage structured data.
export const faq = [
  ['Does Kaasu hold or move my money?', 'No. Customers pay straight into your bank account through any UPI app. Kaasu never holds, routes or touches the money. It only reads your bank alerts and marks the order paid.'],
  ['How does Kaasu know a payment arrived?', 'It checks a signed alert from your bank, from a paired phone or from verified bank email, and matches the amount and time to an open order. If it is not sure, the order waits for your OK.'],
  ['Is Kaasu really free?', 'Yes. Kaasu is free, open source software under the AGPL-3.0 licence. There are no transaction fees, no subscription and no account to create. A commercial licence exists for teams that cannot use the AGPL.'],
  ['Can a fake payment screenshot fool it?', 'No. A screenshot has no effect. An order turns paid only when a verified bank alert matches it.'],
  ['What happens if the bank alert is late?', 'The order stays on Waiting and nothing is lost. When the alert arrives it is matched. You can also check your bank and approve the order yourself, and that decision is recorded.'],
  ['Which languages does Kaasu support?', 'Kaasu works in eleven languages: English, Hindi, Tamil, Telugu, Kannada, Malayalam, Marathi, Bengali, Gujarati, Punjabi and Odia. Amounts can also be spoken aloud at the counter.'],
  ['Can the AI features mark orders as paid?', 'No. AI help is optional, off by default and uses your own key. It never confirms payments, never changes an order and never sees raw bank alert text.'],
  ['Is Kaasu a payment gateway?', 'No. A payment gateway sits in the money flow. Kaasu sits beside it and only confirms what your bank reports, which is why it has no fees and needs no merchant onboarding.'],
] as const;
