# Contributing to Kaasu

Thanks for helping. Kaasu handles people's money, so we keep a high bar and a simple process.

## Ways to help

- **Code:** pick an [issue](https://github.com/opensawce/kaasu/issues). Each has a brief and an end state.
- **New bank formats:** report an alert format we don't parse yet with the *Bank alert format* issue template. Redact it first.
- **Translations:** native speakers reviewing strings in any of our 11 languages.
- **Docs and design:** see [`design.md`](design.md) for UI and [`architecture.md`](architecture.md) for the system.

## Workflow

1. Comment on the issue to claim it, so work isn't duplicated.
2. Read [`AGENTS.md`](AGENTS.md) for conventions and hard rules, and the specs: [`design.md`](design.md) (UI) and [`architecture.md`](architecture.md) (system).
3. Branch from `main`. Use [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`…).
4. Add tests, and a simulator scenario if behaviour changes. `make lint test` must pass offline.
5. Open a PR with the template filled in. One issue per PR, with `Closes #n`.
6. Changes to the money path (`match/`, `parsers/`, `states/`, `ledger/`, `auth/`, ingest) need two approvals, including a code owner.

## Contributor License Agreement

The first time you open a PR, the CLA Assistant bot asks you to sign the [CLA](CLA.md). It's one click and only once. You keep the copyright in your work.

## AI-assisted coding policy

AI tools are welcome. The person who opens the PR is the author and is fully responsible for it.

1. **Understand every line.** Review and be able to explain everything you submit. "The AI wrote it" is not an answer in review.
2. **Disclose.** Fill in the AI section of the PR template. Add an `Assisted-by: <tool>` trailer to commits with substantial AI-generated content.
3. **Money path gets human reasoning.** For `match/`, `parsers/`, `states/`, `ledger/`, `auth/` and ingest, AI may draft but the author must reason through every branch, and at least one test for the core case must be written by hand.
4. **Never paste into AI tools:** secrets, API keys, real bank alerts, customer data, UPI IDs, account numbers or production logs. Use redacted fixtures and `kaasu sim`.
5. **Licensing.** Do not submit AI output you suspect reproduces someone else's code. The CLA makes you responsible for your right to submit it.
6. **No bulk or drive-by PRs.** One issue per PR. Mass AI refactors and PRs without a linked issue will be closed.
7. **Run it.** AI-written code needs tests you have actually run; `make test` passes offline.
8. **Translations and assets.** AI translations need native-speaker review. No AI-generated images in brand assets (logo, Paisu, icons).
9. **AI agents working in this repo** must not push to `main`, merge PRs, sign the CLA, skip checks or edit this policy.
10. Maintainers may close any PR that doesn't follow this policy.

## Security

Never report vulnerabilities in public issues. Use [GitHub Security Advisories](https://github.com/opensawce/kaasu/security/advisories/new).

## Be kind

Assume good intent, review the code not the person, and help newcomers. Maintainers may moderate or lock threads that aren't.
