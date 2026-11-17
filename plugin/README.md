# RegisterDomainCLI plugin for Claude

Find, buy and run domains by asking Claude in plain language. This plugin
connects Claude to [RegisterDomainCLI](https://registerdomaincli.com), a
domain registrar built for AI agents, and teaches Claude how to use it well.

## What it contains

- **MCP connector** (`.mcp.json`): the hosted RegisterDomainCLI MCP server at
  `https://registerdomaincli.com/v1/mcp` (streamable HTTP). The first time
  Claude uses it, a browser window opens and you sign in with your email and
  the 6-digit code we email you. The account is created on first sign-in and
  is the same one the CLI and the web panel use.
- **Skill** (`skills/domain-cli/SKILL.md`): instructions for Claude on searching
  and quoting domains, placing registration, renewal and transfer orders,
  managing DNS records, nameservers, contacts, transfer lock and auto-renew,
  and which actions always need your explicit approval.

## What it runs, sends and fetches

- The plugin has **no hooks and runs no local code** on its own. The only
  network destination it declares is `https://registerdomaincli.com`.
- Tool calls send the arguments of each call (domain names, DNS records,
  contact details you provide) to `registerdomaincli.com` over HTTPS,
  authenticated with the OAuth token from your sign-in. Your conversation is
  not sent.
- When the session has a shell and you agree, the skill may ask Claude to
  install the open-source `domain-cli` binary from this repository's GitHub
  releases with `curl -fsSL https://registerdomaincli.com/install.sh | sh`
  (a single binary in `~/.local/bin`, no root). Claude asks you before
  running it, and with the connector available it can do everything without
  installing anything.

## Payments and safety

- Claude never pays on its own. From Claude, every order ends at a checkout
  link that you open and pay in your own browser, even when a payment method
  is saved on your account.
- Read-only tools (search, quotes, listings) run without prompting. Tools
  that change your account, spend money or reveal a transfer code are marked
  as such, and Claude asks for your permission before each call; placing an
  order, unlocking a domain, enabling auto-renew, revealing an EPP code,
  editing or deleting DNS records and changing nameservers or contacts also
  require an explicit confirmation.
- Account deletion, payment-method management and the billing portal are not
  available through the plugin.

## Documentation and support

- Connector documentation: https://registerdomaincli.com/en/docs/claude
- Privacy policy: https://registerdomaincli.com/en/legal/privacy
- Terms: https://registerdomaincli.com/en/legal/terms
- Support: support@registerdomaincli.com

## License

Apache-2.0, see [LICENSE](LICENSE).
