# Security Policy

RegisterDomainCLI moves money and handles personal data (registrant
contacts, billing profiles). Report vulnerabilities privately.

**Please do not open public issues for security reports.**

- Contact: **security@registerdomaincli.com**
- Include: a description, reproduction steps, the affected component, and your
  assessment of severity.
- We aim to acknowledge reports within 72 hours and will coordinate a fix and
  disclosure timeline with you.

## Scope

- The CLI (this repository): command behavior, token storage on disk, install
  script integrity, release artifact integrity.
- The server (private repository): API, auth, payments, invoicing. Reports
  about endpoints reachable through the public CLI are welcome here too.

## Handling

- API tokens (`dcl_...`) are stored with `0600` permissions in
  `os.UserConfigDir()/domain-cli/config.json`. Treat them like passwords.
- The CLI never stores card data; payments happen on hosted payment pages.
- Known accepted risk for v1: the web panel stores the API token in
  `localStorage` (documented in the server repository's plan), mitigated by a
  strict CSP. A separate httpOnly panel session is post-v1.

## Out of scope

- Automated scanner output without a concrete exploit path.
- Social engineering of registrar or payment-provider accounts.
