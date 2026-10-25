# RegisterDomainCLI

An open-source Go CLI to search, register, renew, transfer and manage DNS for
domain names — paid via the hosted checkout page, with the customer as the
Registered Name Holder.

```sh
curl -fsSL https://registerdomaincli.com/install.sh | sh
```

## Quickstart

```sh
domain-cli signup                  # email + 6-digit code from your inbox
domain-cli login                   # same flow, for an account you already have
domain-cli account list            # saved sign-ins; `account switch` moves between them
domain-cli logout                  # revoke this machine's token
domain-cli search mybrand          # keyword search across TLDs
domain-cli register mybrand.com    # quote, checkout, registration
domain-cli dns set mybrand.com A @ 203.0.113.10
```

`domain-cli --help` lists every command (`signup`, `login`, `logout`, `whoami`,
`account`, `search`, `register`, `renew`, `transfer`, `orders`, `invoice`,
`contacts`, `billing`, `domains`, `dns`, `legal`).

After each successful command the CLI compares its version against the latest
GitHub release (at most once every 24 hours, result cached in the config
directory) and prints a stderr line when an update exists or the check fails.
Set `DOMAIN_CLI_NO_UPDATE_CHECK=1` to disable it.

## What is open here

This repository contains only the CLI, the shared wire contract
(`apitypes/`), the agent-harness skill, the install script and release CI —
Apache-2.0. The web frontend and the backend live in a private repository and
are **not** covered by this license.

## Install from source

```sh
go install github.com/registerdomaincli/cli/cmd/domain-cli@latest
```

Requires Go ≥ 1.27.

## Security

See [SECURITY.md](SECURITY.md). Report vulnerabilities privately — do not
open public issues.

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
