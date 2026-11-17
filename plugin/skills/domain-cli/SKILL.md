---
name: domain-cli
description: Search, register, renew, transfer, and manage DNS for domains via RegisterDomainCLI — use when the user wants to find or buy a domain, or manage DNS records/nameservers for domains they own there. The user just asks in plain language; you run every step for them
---

# RegisterDomainCLI — domain search, registration, and DNS from the CLI

`domain-cli` searches domain availability, registers/renews/transfers
domains through RegisterDomainCLI, and manages DNS records, nameservers,
contacts, and billing for domains the user owns there.

## Do the work for the user — they should not need to learn commands

The whole point of this skill is that the human just asks — "I want to
register a domain through registerdomaincli.com for my bakery", "point
example.com at my server", "renew it for two years" — and you do the rest.

- **Run the commands yourself.** Do not answer with a list of `domain-cli`
  commands for the human to type. Show or explain a command only when they
  ask how it works or say they want to run it themselves.
- **Missing pieces are your job too.** No `domain-cli` on the machine →
  install it (see **Install**), telling the human first. No account → run
  `signup` and ask them only for the emailed 6-digit code. No contact set or
  billing profile → ask for the data in plain language and save it.
- **Turn intent into steps.** "Something short for my bakery" → search
  sensible keywords and TLDs (`domain-cli tlds --group …`), shortlist a few
  with prices, let them pick. "Set up email" → ask for their provider and
  create the records it documents.
- **The human only does what must be human:** type the emailed code, approve
  the quoted total and the payment method, complete payment in their own
  browser, and confirm the guarded actions described below (unlock/auth
  code, nameserver changes, record deletion, contact changes, autorenew on).
  Everything else you do without asking them to touch a terminal.
- **Have a shell? Use the CLI, and install it yourself.** That is why
  RegisterDomainCLI ships a CLI and not only an MCP server: installing
  `domain-cli` is one command you can run on your own once the human says
  yes, with no configuration and no restart. Adding an MCP server usually
  has to be done by hand by the human, and even when you can edit the
  config yourself the harness normally needs a restart before the tools
  show up. So never send a human with a shell off to set up the MCP
  connector. Install the CLI instead. (If the connector is already
  connected in this session, using it is fine.)
- **No shell?** Same rule, through the hosted MCP server tools.

## Install

Ask the human once ("Can I install domain-cli? It is a single binary in
~/.local/bin, no root needed"), then run the command yourself:

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
  `domain-cli signup --email user@example.com` (sends the code), then pass
  the code the human typed through a private file with `--code-file`, never
  as `--code` (process arguments are readable by other local users):
  1. Create the file: `umask 077; mktemp "${TMPDIR:-/tmp}/domain-cli-code.XXXXXX"`
     (prints its path).
  2. Write the 6 digits into that path with your file-writing tool (for
     example Claude Code's Write tool), not with a shell command, so the code
     never appears in a command line.
  3. Verify and clean up, keeping the verification's exit status:
     ```sh
     f=/path/printed/in/step/1
     domain-cli signup --email user@example.com --code-file "$f"; s=$?; rm -f "$f"; exit $s
     ```
  If you only have a shell, `printf '%s\n' 123456 > "$f"` works but puts the
  code in that shell's arguments for a moment. The code is single-use and
  expires in 10 minutes, so do that only on a single-user machine;
  otherwise use the MCP connector's browser sign-in. Do not pass `--json` to
  this verification step: its JSON output contains the new token, which is
  already saved privately in the config.
- **Signing in again** is `domain-cli login`, the identical two-step flow
  (`--email`, then `--email` + `--code-file`, exactly as above, without
  `--json`). It saves the sign-in as a switchable
  account: `domain-cli account list` shows the saved sign-ins and
  `domain-cli account switch <email>` moves between them without another code.
- **Signing out** is `domain-cli logout`: it revokes this machine's token on
  the server and forgets the sign-in. Other machines signed in to the same
  account hold their own tokens and stay signed in. `logout` and
  `domain-cli account remove <email>` need no `--yes` — they only remove
  access, and they never delete anything on the server.
- **Ordering requires explicit human approval**: pass the global `--yes` only
  after the human confirmed the quoted price. Without it the command fails by
  design. **Always pass `--no-wait` when you place an order**: without it the
  CLI keeps polling for up to 15 minutes (and appends status lines after the
  `--json` object), so you could not hand the checkout link to the human
  until it gives up. With `--no-wait` it prints the order (id +
  `checkoutUrl`) and exits; follow up with `domain-cli orders` or the
  `get_order` MCP tool. An order normally
  answers with a `checkoutUrl` the human completes in the browser; an order
  answered with an **empty `checkoutUrl`** means the saved payment method is
  being charged off-session — the order completes by itself through the
  provider webhook, no browser involved.
  **Saved methods — ask before relying on them**: before the first
  `register`/`renew`/`transfer` of a session, run `domain-cli payment-methods`
  (or `--json`). If it lists a method, tell the human and ask which they want:
  pay with the saved one (instant, no browser — run the order normally), or
  pay with a different one (`--new-payment-method` opens the hosted checkout,
  which also saves the new method for next time). If none is listed, just
  proceed. **If you are Claude** (claude.ai, the Claude apps or Claude Code,
  including through the RegisterDomainCLI plugin), never charge a saved
  method: always pass `--new-payment-method`, so every purchase ends at a
  checkout link the human pays in their browser — the same rule the MCP
  connector enforces for Claude. Get the exact total right before ordering
  with `--dry-run --json` (the quote's `totalCents`), have the human approve
  it, and place the order straight away. Never silently charge a saved method without the human having
  agreed in this conversation; methods are removed in the web panel, not from
  the CLI.
- **Editing an order is cancel + recreate**: an unpaid order can be dropped
  with `domain-cli orders --cancel <id>` and placed again with the full
  domain list (add or remove domains by re-running `register` with the
  wanted set). There is no patch endpoint — the payment session is immutable
  once created.
- **Registrant contacts** (registration/transfer): pass `--contact <label>`
  or the full flag set (`--first-name --last-name --email --phone --address1
  --city --zip --country`, optional `--organization --address2 --state`);
  `--save-contact <label>` persists them for reuse.
- **Per-role WHOIS contacts** (registration/transfer): registries carry
  admin, tech and billing contacts besides the registrant. By default all
  three are the registrant (what almost everyone wants; privacy services
  hide them anyway). To differentiate a role, pass `--role-<name>
  field=value,...` (`admin`, `tech`, `billing`; fields are the registrant
  flags) or `--role <name>=<spec>`; `auto`/`registrant` pin the default
  explicitly. Field specs derive from the INLINE registrant only — with a
  contact set they are rejected. A spec value cannot contain a comma (the
  comma separates field=value pairs); escape none, pass another flag pair
  instead. Values must respect the registrant contract: required fields
  non-empty, country two ASCII letters.
- **Billing profile & VAT** (before the first order for a new account): run
  `domain-cli billing show`. When it prints "No billing profile saved", the
  server bills from the domain's registrant contact and charges the default
  VAT rate (no reverse charge / no exemption). Ask the human for their billing
  data (legal name, address, and VAT/tax ID if any) and save them with
  `domain-cli billing set`. VAT follows the buyer's country: buyers in Spain
  always pay Spanish VAT; businesses in another EU country whose VAT ID
  passes VIES verification get the reverse charge (0%); buyers outside the
  EU pay no VAT. Never promise an exemption — the quote shows the result.
  Quotes carry `netCents`/`taxCents`/`totalCents`: always confirm the
  `totalCents` figure with the human before `--yes`.
- **JSON output**: in the current release the global `--json` flag yields
  exactly one JSON object only for: `whoami`, `tlds`, `orders` (listing),
  `payment-methods`, `domains contacts <domain>`, the `account` subcommands,
  `logout`, the code-request step of `signup`/`login`, and
  `register`/`renew`/`transfer` with `--dry-run` (the quote) or with
  `--no-wait` and no `--save-contact` (the order). Every other command
  ignores `--json` or mixes JSON with text, so treat its output as
  human-readable and do not re-run a mutation because its output did not
  parse — check the result with a read instead. For structured reads use the
  read-only API calls below (`/v1/domains/search`, `/v1/domains`,
  `/v1/domains/<domain>/records`, `/v1/billing`, …) or the matching MCP
  tools.
- **Currency**: pass the global `--currency <code>` (or `DOMAIN_CLI_CURRENCY`)
  so every price — search rows, quotes, orders — is shown and billed in the
  user's currency. Pick the currency from the user's locale/location when they
  have not named one (`eur` for the euro area, `usd` for the United States,
  …). Unsupported codes fail with an explicit server error; search results and
  quotes always carry a `currency` field, so never assume one. Prices in
  search output are indicative; the exact charge is the quote's
  `totalCents` (VAT included; `netCents` is before tax) in the quote's own
  `currency`.
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
same account, same dcl_ token, same API. This path is for agents WITHOUT a
shell: with a shell, install the CLI (see above) rather than asking the
human to configure a connector and restart.

- **Endpoint**: `https://registerdomaincli.com/v1/mcp` (MCP over streamable
  HTTP, JSON-RPC; protocol version 2025-06-18).
- **Auth — prefer OAuth**: connector clients (Claude, Gemini) should run the
  OAuth flow: any request without a valid token gets an HTTP 401 carrying
  `WWW-Authenticate: Bearer resource_metadata=…` → protected-resource
  metadata → authorization-server metadata at
  `https://registerdomaincli.com/.well-known/oauth-authorization-server/v1/mcp/oauth`
  → Claude's published client identity (CIMD) or dynamic client registration
  (`POST /v1/mcp/oauth/register`, public client) → open
  `/v1/mcp/oauth/authorize` in the human's browser: they sign
  in with email + the emailed 6-digit code (the same login as the CLI and
  the panel) → exchange the code (PKCE S256) at `/v1/mcp/oauth/token`. The
  access token is a normal dcl_ token. No token copying for the human.
  A static `Authorization: Bearer dcl_…` header keeps working too.
- **Tools** mirror the CLI: `whoami`, `search_domains` (`tlds`/`limit`
  filters), `list_tlds`, `quote`, `quote_domains` (price several domains in
  one call), `create_order` (`domains[]` buys several domains in ONE
  order: one payment; `discount_code` on the quote and order tools),
  `list_orders`, `get_order`, `get_invoice`, `cancel_order`,
  `list_domains`, `get_domain`, `set_lock`, `set_autorenew`,
  `get_auth_code`, `get_domain_contacts`, `set_domain_contacts`,
  `list_records`, `create_record`, `update_record`, `delete_record`,
  `get_nameservers`, `set_nameservers`, `list_contact_sets`,
  `create_contact_set`, `set_default_contact_set`, `delete_contact_set`,
  `get_billing`, `set_billing`, `list_payment_methods`. Account deletion
  stays in the CLI and the web panel.
- **confirm=true is the --yes of MCP**: `create_order`, `cancel_order`,
  `update_record`, `delete_record`, `set_nameservers`, `get_auth_code`,
  `set_domain_contacts`, `delete_contact_set` refuse without
  `confirm: true`; `set_lock` needs it to unlock; `set_autorenew` needs it
  to enable. Ask the human in chat first, exactly as with `--yes`.
- **From Claude, payment always goes through checkout**: when the connector
  is used from Claude (claude.ai, the Claude apps or Claude Code signed in
  through OAuth), `create_order` never charges a saved payment method — it
  always returns a `checkoutUrl` for the human to pay in their own browser,
  and `use_saved_method` is not offered. Hand the link over and stop.
- **Payment is a human decision** (other MCP clients): before the first
  `create_order` of a session, call `list_payment_methods`. If one is saved,
  ask the human:
  charge it (default — the order completes by itself, no browser), or pay
  with something new (pass `use_saved_method: false` and `create_order`
  returns `checkoutUrl`). Either way give the human the outcome and stop;
  never automate a payment page, the billing portal, or invoices. An empty
  `checkoutUrl` in the response means the saved method was charged — report
  the order id and poll `get_order`.
- **Signup has no MCP shortcut**: the emailed 6-digit code must still be
  typed by the human (CLI, web panel, or the `auth/signup` + `auth/verify`
  REST pair from an environment the human controls).

If `--server`/`DOMAIN_CLI_SERVER_URL` is unset interactively, the CLI prompts
(defaults: `http://localhost:8080` in dev, `https://registerdomaincli.com` in
production).

## Command reference

| Command | What it does |
|---|---|
| `domain-cli search <query> [--tlds com,net,...] [--limit N]` | Keyword search, or FQDN check when the query contains a dot. `--tlds` restricts the result to those TLDs (≤50, run `tlds` to see the catalog); `--limit` caps rows (default and ceiling 50). Prices are indicative; exact charge appears at `register`/`renew`/`transfer` confirm time. |
| `domain-cli tlds [--group <slug>]` | List every sellable TLD with its curated intent groups (`local`, `business`, `tech`, `shop`, `creative`, `org`). `--group` filters the list. Use it to translate "domains in the user's region" into an exact `--tlds` set. |
| `domain-cli register <domain...> [--years N] [--own-contact \| --contact <label>] [--role-admin <spec>] [--role-tech <spec>] [--role-billing <spec>] [--new-payment-method] [--dry-run]` | Quote → confirm → order → checkout in the human's browser → polls until paid/failed. With a saved payment method and no `--new-payment-method` the order is charged instantly (empty `checkoutUrl`, no browser). With SEVERAL domains they go into ONE order (one payment, one invoice with a line per domain). Prompts for registrant when no default contact set exists; offers to save it. |
| `domain-cli renew <domain> [--years N] [--new-payment-method]` | Same flow with `kind=renewal`. |
| `domain-cli transfer <domain> --auth-code <code> [--contact <label>] [--own-contact] [--new-payment-method]` | Same flow with `kind=transfer`. Transfers take up to 7 days; watch `domain-cli orders`. `--auth-code` is a process argument other local users can read while the command runs: on a shared machine place the transfer through the MCP `create_order` tool (`kind=transfer`) or the web panel instead. |
| `domain-cli orders [--cancel <id>]` | List orders (status, invoice number, checkout URL) or cancel an open order. |
| `domain-cli invoice <orderId>` | Opens the order's pre-signed invoice URL in the browser and prints it. |
| `domain-cli contacts add <label> \| list \| show <label> \| default <label> \| remove <label>` | Manage saved registrant contact sets. `add` accepts the same `--role-*` flags; `show` prints the per-role summary. |
| `domain-cli payment-methods` | List the saved payment methods that power the no-browser fast checkout. Check this before the first order of a session and ask the human which rail to pay on; `--new-payment-method` on the order forces the hosted checkout instead. |
| `domain-cli legal [notice\|privacy\|terms\|cookies]` | Open a legal document (default `notice`). |
| `domain-cli domains` | List owned domains (expiry, autorenew, privacy, lock). |
| `domain-cli domains authcode <domain>` | Print the transfer (EPP) code. Asks for confirmation; the domain must be unlocked first (`domains unlock`). |
| `domain-cli domains lock <domain>` / `unlock <domain>` | Toggle transfer lock. `unlock` asks for confirmation; `lock` does not. |
| `domain-cli domains contacts <domain> [--contact <label> \| --own-contact \| --role-<name> <spec>] [--dry-run]` | Show the live WHOIS contacts of an owned domain, or replace them (asks for confirmation; `--yes` non-interactively). A registrant change may trigger ICANN email verification and a registrar transfer lock — tell the human before applying it. MCP: `get_domain_contacts` / `set_domain_contacts`. |
| `domain-cli domains autorenew <domain> on\|off` | Toggle autorenew. Turning it **on** authorises a recurring annual charge and asks for confirmation. |
| `domain-cli dns list <domain>` | List DNS records. |
| `domain-cli dns set <domain> <TYPE> <host> <answer> [--ttl 3600]` | Create a DNS record. |
| `domain-cli dns delete <domain> <recordID>` | Delete a DNS record by numeric id. |
| `domain-cli dns ns get <domain>` / `ns set <domain> <ns1> <ns2> [...]` | Show/replace nameservers. |
| `domain-cli login [--email <addr>] [--code-file <path>]` | Same two-step flow as `signup` (code through a private file, never `--code`; no `--json` on the verification step); saves the sign-in as a switchable account. |
| `domain-cli logout` | Revoke this machine's stored token and forget the sign-in. |
| `domain-cli account list \| switch <email> \| remove <email> \| delete` | List saved sign-ins, switch between them, revoke-and-forget one, or delete the account entirely. |

## Payment ALWAYS requires a human decision — and usually a human in the browser

**The agent must print the checkout URL and stop. Never attempt to open,
automate, fill, or complete a payment page yourself** — checkout is a
hosted payment page the human completes in their own browser. The same rule
applies to `billing portal` and `invoice`. `register`/`renew`/`transfer`
already do the right thing: they open the browser (or print the URL over
SSH) and poll the order status.

Two sanctioned exceptions where no browser is involved, both because the
human already consented to THAT charge in this conversation:

- The order was confirmed (`--yes` after the human approved the exact price)
  AND the human chose to pay with the saved method (see the ask-first rule
  above): the order answers with an empty `checkoutUrl`, the saved method is
  charged off-session, and the provider webhook completes the order.
- `domains autorenew on` commits the user to a recurring annual charge on
  their saved payment method — always confirm with the human before running
  it for them.

Anything beyond that — retries after a decline, charges "to try if it
works", or re-running an order that already consumed the saved method — is
forbidden without a fresh, explicit human instruction.

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

# Search — optionally restricted to TLDs and capped
curl -sS -X POST "$BASE/v1/domains/search" \
  -H @"$HDR" -H "Content-Type: application/json" \
  -d '{"query":"example","tlds":["com","net","org"],"limit":50}'

# TLD catalog: curated groups + sellable TLDs (?group=<slug> filters)
curl -sS "$BASE/v1/tlds" -H @"$HDR"

# Quote (exact pre-order price; years ignored for transfers)
curl -sS "$BASE/v1/quote?domain=example.com&years=1&kind=registration" -H @"$HDR"

# List DNS records
curl -sS "$BASE/v1/domains/example.com/records" -H @"$HDR"
```

Signing out is NOT part of that read-only script: use `domain-cli logout`,
only when the human asks to sign out. It revokes this machine's token on the
server AND clears it from the config; a raw `POST /v1/auth/logout` would
revoke the token but leave it in the config, breaking every later CLI
command until the next `login`.

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
