---
name: domain-cli
description: Search, register, renew, transfer, and manage DNS for domains via RegisterDomainCLI — use when the user wants to find or buy a domain, or manage DNS records/nameservers for domains they own there
---

# RegisterDomainCLI — domain search, registration, and DNS from the CLI

`domain-cli` searches domain availability, registers/renews/transfers
domains through RegisterDomainCLI, and manages DNS records, nameservers,
contacts, and billing for domains the user owns there.

## Install

- Linux/macOS: `curl -fsSL https://registerdomaincli.com/install.sh | sh`
- Windows (PowerShell): download `domain-cli-windows-amd64.exe` from the
  [latest release](https://github.com/registerdomaincli/cli/releases/latest),
  unblock it, and add it to `PATH` (the landing page lists the same steps).
- Fallback with Go toolchain: `go install github.com/registerdomaincli/cli/cmd/domain-cli@latest`
- **Android (Termux)**: the static `linux/arm64` release binary runs as-is
  inside Termux (`pkg install curl`, then the install command above, with
  `~/.local/bin` on `PATH` for the fallback). No root needed. **iOS has no
  local execution option** — there is no terminal app that can run the
  binary; on phones without a real shell, use the hosted MCP server or the
  web panel instead of `install.sh`.

## Agent contract (non-interactive use)

Every command runs without a TTY. When required flags are missing the CLI
exits immediately with `missing required flag(s): …` — it never hangs.

- **Signup** is two-step (the emailed 6-digit code must be typed by the
  human — **the agent must NOT automate mailbox access or guess the code**):
  `domain-cli signup --email user@example.com` (sends the code), then
  `domain-cli signup --email user@example.com --code 123456` (saves the token).
- **Signing in again** is `domain-cli login`, the identical two-step flow
  (`--email`, then `--email` + `--code`). It saves the sign-in as a switchable
  account: `domain-cli account list` shows the saved sign-ins and
  `domain-cli account switch <email>` moves between them without another code.
- **Signing out** is `domain-cli logout`: it revokes this machine's token on
  the server and forgets the sign-in. Other machines signed in to the same
  account hold their own tokens and stay signed in. `logout` and
  `domain-cli account remove <email>` need no `--yes` — they only remove
  access, and they never delete anything on the server.
- **Ordering requires explicit human approval**: pass the global `--yes` only
  after the human confirmed the quoted price. Without it the command fails by
  design. `--no-wait` returns right after order creation with the checkout
  URL — the human completes payment in the browser asynchronously.
- **Registrant contacts** (registration/transfer): pass `--contact <label>`
  or the full flag set (`--first-name --last-name --email --phone --address1
  --city --zip --country`, optional `--organization --address2 --state`);
  `--save-contact <label>` persists them for reuse.
- **Billing profile & VAT** (before the first order for a new account): run
  `domain-cli billing show`. When it prints "No billing profile saved", the
  server bills from the domain's registrant contact and charges the default
  VAT rate (no reverse charge / no exemption). Ask the human for their billing
  data (legal name, address, and VAT/tax ID if any) and save them with
  `domain-cli billing set` — for EU businesses a valid VAT ID makes the server
  apply the reverse charge (0% VAT) instead of the domestic rate.
  Quotes carry `netCents`/`taxCents`/`totalCents`: always confirm the
  `totalCents` figure with the human before `--yes`.
- **JSON output**: the global `--json` flag prints the API response object
  instead of the human table — prefer it for parsing.
- **Currency**: pass the global `--currency <code>` (or `DOMAIN_CLI_CURRENCY`)
  so every price — search rows, quotes, orders — is shown and billed in the
  user's currency. Pick the currency from the user's locale/location when they
  have not named one (`eur` for the euro area, `usd` for the United States,
  …). Unsupported codes fail with an explicit server error; search results and
  quotes always carry a `currency` field, so never assume one. Prices in
  search output are indicative; the exact charge is the quote's `netCents`
  in the quote's own `currency`.
- **First run**: pass `--server <url>` or set `DOMAIN_CLI_SERVER_URL`;
  without a TTY the CLI refuses to guess a server.
- **The server must be https** (plain http is accepted only for localhost).
  A token saved by `signup` belongs to the server it was saved against:
  pointing `--server`/`DOMAIN_CLI_SERVER_URL` at a different host fails
  unless you also pass `--token` explicitly.
- **Keep secrets off the command line**: `--token-file <path>` and
  `signup --code-file <path>` read the token and the 6-digit code from a
  file, where other local users cannot read them out of the process table.
- **Version currency**: if the tool is already installed, check it is not
  stale — `domain-cli --version`, once per session (not on every message),
  and only re-check after the user installs something or a day has passed.
  A release newer than the printed `vX.Y.Z` means the install is behind:
  run the install command from **Install** again (it is idempotent), or
  `go install …@latest` for source installs. Do not silently continue on a
  stale binary when the task touches billing or DNS mutations; tell the
  user and proceed only on their word. `dev`/non-`v*` versions have no
  release line — skip the check.

## No shell available? Use the MCP server

Mobile agent apps (Claude, Gemini) and other environments without a real
terminal cannot run `domain-cli`. Talk to the hosted MCP server instead —
same account, same dcl_ token, same API:

- **Endpoint**: `https://registerdomaincli.com/v1/mcp` (MCP over streamable
  HTTP, JSON-RPC; protocol version 2025-06-18).
- **Auth**: every `tools/call` needs `Authorization: Bearer dcl_…` — the
  token `signup`/`login` minted (the human reads it from the web panel or a
  machine where the CLI is installed). `initialize` and `tools/list` are
  public.
- **Tools** mirror the CLI: `whoami`, `search_domains`, `quote`,
  `create_order`, `list_orders`, `get_order`, `cancel_order`,
  `list_domains`, `get_domain`, `set_lock`, `set_autorenew`,
  `get_auth_code`, `list_records`, `create_record`, `delete_record`,
  `get_nameservers`, `set_nameservers`, `list_contact_sets`,
  `create_contact_set`, `get_billing`, `set_billing`.
- **confirm=true is the --yes of MCP**: `create_order`, `cancel_order`,
  `delete_record`, `set_nameservers`, `get_auth_code` refuse without
  `confirm: true`; `set_lock` needs it to unlock; `set_autorenew` needs it
  to enable. Ask the human in chat first, exactly as with `--yes`.
- **Payment is still human-only**: `create_order` returns `checkoutUrl`
  plus a `hint` — give the URL to the human and stop. Never automate a
  payment page, the billing portal, or invoices.
- **Signup has no MCP shortcut**: the emailed 6-digit code must still be
  typed by the human (CLI, web panel, or the `auth/signup` + `auth/verify`
  REST pair from an environment the human controls).

If `--server`/`DOMAIN_CLI_SERVER_URL` is unset interactively, the CLI prompts
(defaults: `http://localhost:8080` in dev, `https://registerdomaincli.com` in
production).

## Command reference

| Command | What it does |
|---|---|
| `domain-cli search <query>` | Keyword search, or FQDN check when the query contains a dot. Prices are indicative; exact charge appears at `register`/`renew`/`transfer` confirm time. |
| `domain-cli register <domain> [--years N] [--own-contact \| --contact <label>] [--dry-run]` | Quote → confirm → order → checkout in the human's browser → polls until paid/failed. Prompts for registrant when no default contact set exists; offers to save it. |
| `domain-cli renew <domain> [--years N]` | Same flow with `kind=renewal`. |
| `domain-cli transfer <domain> --auth-code <code> [--contact <label>] [--own-contact]` | Same flow with `kind=transfer`. Transfers take up to 7 days; watch `domain-cli orders`. |
| `domain-cli orders [--cancel <id>]` | List orders (status, invoice number, checkout URL) or cancel an open order. |
| `domain-cli invoice <orderId>` | Opens the order's pre-signed invoice URL in the browser and prints it. |
| `domain-cli contacts add <label> \| list \| show <label> \| default <label> \| remove <label>` | Manage saved registrant contact sets. |
| `domain-cli billing show \| set \| portal` | View/edit the invoice billing profile; open the hosted billing portal. |
| `domain-cli legal [notice\|privacy\|terms\|cookies]` | Open a legal document (default `notice`). |
| `domain-cli domains` | List owned domains (expiry, autorenew, privacy, lock). |
| `domain-cli domains authcode <domain>` | Print the transfer (EPP) code. Asks for confirmation; the domain must be unlocked first (`domains unlock`). |
| `domain-cli domains lock <domain>` / `unlock <domain>` | Toggle transfer lock. `unlock` asks for confirmation; `lock` does not. |
| `domain-cli domains autorenew <domain> on\|off` | Toggle autorenew. Turning it **on** authorises a recurring annual charge and asks for confirmation. |
| `domain-cli dns list <domain>` | List DNS records. |
| `domain-cli dns set <domain> <TYPE> <host> <answer> [--ttl 3600]` | Create a DNS record. |
| `domain-cli dns delete <domain> <recordID>` | Delete a DNS record by numeric id. |
| `domain-cli dns ns get <domain>` / `ns set <domain> <ns1> <ns2> [...]` | Show/replace nameservers. |
| `domain-cli login [--email <addr>] [--code <6 digits>]` | Same two-step flow as `signup`; saves the sign-in as a switchable account. |
| `domain-cli logout` | Revoke this machine's stored token and forget the sign-in. |
| `domain-cli account list \| switch <email> \| remove <email> \| delete` | List saved sign-ins, switch between them, revoke-and-forget one, or delete the account entirely. |

## Payment ALWAYS requires a human in the browser

**The agent must print the checkout URL and stop. Never attempt to open,
automate, fill, or complete a payment page yourself** — checkout is a
hosted payment page the human completes in their own browser. The same rule
applies to `billing portal` and `invoice`. `register`/`renew`/`transfer`
already do the right thing: they open the browser (or print the URL over
SSH) and poll the order status.

`domains autorenew on` commits the user to a recurring annual charge on
their saved payment method — always confirm with the human before running
it for them.

`domains unlock` and `domains authcode` are the two halves of the
transfer-away path: together they let the domain be moved to another
registrar. Both ask for confirmation, and non-interactively they require
`--yes` exactly like ordering. Never run them without an explicit
instruction naming the domain.

## Direct API access

The CLI talks to the same API an agent can call with `curl`. All endpoints
are under `/v1`. `auth/signup`, `auth/verify`, `healthz` and `readyz` are
public; everything else needs `Authorization: Bearer dcl_...` (the token
saved by `signup`).

**Do not hardcode the base URL.** The token in the config file belongs to
the server recorded next to it, and the CLI refuses to send one anywhere
else (`tokenBelongsToServer`). A snippet that reads the token but points at
a fixed host does the very thing that guard exists to prevent: a session
saved against a staging or self-hosted server would hand that server's
credential to production. Read `server_url` from the same file.

The token lives at `os.UserConfigDir()/domain-cli/config.json`, which is
`$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/Application Support`
on macOS, and `%AppData%` on Windows — do not hardcode one of them.

The same rule as `--token-file` applies here: a token expanded into `-H
"Authorization: Bearer $TOKEN"` becomes one of curl's process arguments, and
`/proc/<pid>/cmdline` is world-readable, so any other local user can read it
out of the process table while the request runs. Write the header to a file
once and let curl read it with `-H @`. Run this as a script, not pasted into
an interactive shell: `set -e` is what stops a missing or unreadable config
from being followed by four unauthenticated requests.

```sh
set -e
# The token goes straight from the config file into the header file: it is
# never a process argument, and umask keeps the header file private. BASE
# comes out of that same file, never hardcoded — see the warning above.
umask 077
# The template is not decoration: BSD/macOS mktemp does not accept a bare
# `mktemp` on every release the install section supports.
HDR=$(mktemp "${TMPDIR:-/tmp}/domain-cli-hdr.XXXXXX")
trap 'rm -f "$HDR"' EXIT
BASE=$(python3 - "$HDR" <<'PY'
import json, os, sys
from urllib.parse import urlparse
# Mirrors the CLI's os.UserConfigDir(), so this works from a POSIX shell on
# Windows too: %AppData% there, Application Support on macOS (which ignores
# XDG_CONFIG_HOME), and XDG_CONFIG_HOME or ~/.config everywhere else.
if sys.platform == "win32":
    base = os.environ["APPDATA"]
elif sys.platform == "darwin":
    base = os.path.expanduser("~/Library/Application Support")
else:
    base = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
cfg = json.load(open(os.path.join(base, "domain-cli", "config.json")))
token, server = cfg.get("token"), cfg.get("server_url")
# `logout` leaves "token": "" behind rather than dropping the key, so an
# empty string is the ordinary signed-out state, not a corrupt file. Without
# this check it would go out as an empty bearer and come back 401.
if not isinstance(token, str) or not token:
    raise SystemExit("signed out: run `domain-cli login --email you@example.com`")
server = server.rstrip("/") if isinstance(server, str) else ""
# The token is bound to the server that issued it, so the base URL is read
# from the config rather than hardcoded. These are validExternalURL's checks
# in full: userinfo rejected outright, https needs a host, http only for
# loopback.
u = urlparse(server)
if u.username is not None or u.password is not None or not (
    (u.scheme == "https" and u.hostname)
    or (u.scheme == "http" and u.hostname in ("localhost", "127.0.0.1", "::1"))):
    raise SystemExit("config has no usable server_url: %r" % server)
open(sys.argv[1], "w").write("Authorization: Bearer " + token + "\n")
print(server)
PY
)

# Search
curl -sS -X POST "$BASE/v1/domains/search" \
  -H @"$HDR" -H "Content-Type: application/json" \
  -d '{"query":"example"}'

# Quote (exact pre-order price; years ignored for transfers)
curl -sS "$BASE/v1/quote?domain=example.com&years=1&kind=registration" -H @"$HDR"

# List DNS records
curl -sS "$BASE/v1/domains/example.com/records" -H @"$HDR"

# Sign out — revokes exactly this token, leaving other machines signed in
curl -sS -X POST "$BASE/v1/auth/logout" -H @"$HDR"
```

Prefer the CLI for interactive flows (signup, orders, anything with
prompts); the API is fine for read-only checks.

## Errors and rate limits

- Errors are always `{"error":"<message>","code":"<machine code>"}`; the CLI
  prints the upstream message verbatim. Treat `code` values (e.g.
  `contacts_required`, `domain_not_registrable`) as the stable signal.
- HTTP 429 responses carry a `Retry-After` header; the CLI prints
  "rate limited, retry after Ns". When you hit one, wait at least N seconds
  and retry the same command once before reporting failure.
- If the CLI is not signed in it prints "Not logged in. Run
  `domain-cli signup` first." — do not try to bypass it; `login` runs the same
  flow for an account that already exists.
