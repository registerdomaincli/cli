# Repository Guidelines

## Project Overview

`domain-cli` is the public half of RegisterDomainCLI: a Cobra-based Go CLI that searches, registers, renews and transfers domains and manages DNS, contacts and billing against the RegisterDomainCLI HTTP backend. Module `github.com/registerdomaincli/cli`, Go 1.27, Apache-2.0.

This repo holds only: the CLI, the shared wire contract (`apitypes/`), the agent skill, `install.sh`, and release CI. All pricing, registrar integration, Stripe and fiscal logic lives server-side (`README.md:22-27`).

## Architecture & Data Flow

`cmd/domain-cli/main.go` → `cli.NewRootCmd(version).Execute()` → `os.Exit(1)` on error. **Exit codes are only 0 and 1.**

```
main.go ──> NewRootCmd ──> PersistentPreRunE: Load() config + env + flags
                             ──> *App{Server, Token, Client, p *prompter}
                          ──> 16 explicit root.AddCommand(newXxxCmd(app))
RunE ──> app.requireClient() ──> Client.<Endpoint>() ──> Client.do() ──> /v1 JSON
     ──> fail(err) ──> emitJSON(--json) | i18n.T(...) to cmd.OutOrStdout()
```

- **`*App` is the DI vehicle.** Built once in `PersistentPreRunE` (`cli/root.go:58-85`), captured by closure in every `newXxxCmd(app *App)`. No interfaces are defined in package `cli`.
- **Client is a thin JSON transport** (`cli/client.go:39-121`): 30s timeout, no retries, **no `context.Context` anywhere**. Every request sets `Accept: application/json`, `Accept-Language: <detected locale>` and `Authorization: Bearer dcl_…` when a token exists. The detected CLI locale drives server-side email/invoice localization.
- **Errors are typed pointers, no sentinels**: `*APIError{StatusCode, Message, Code}` and `*RateLimitedError{Seconds}` (from `Retry-After`). Matched with `errors.As`.
- **Order flow** (`cli/orderflow.go:29-137`): `GET /v1/quote` → print confirm → `--dry-run` exits → `requireYes` → `POST /v1/orders` → open/print Stripe `checkoutURL` → `--no-wait` returns → `pollOrder` (3s interval, 15min cap) prints only on status *change*.
- **Payment is always a human in a browser.** Never automate checkout, `billing portal` or `invoice`. `invoice` opens the pre-signed `Order.InvoiceURL` only — a browser carries no bearer token.
- **Signup is two-step and must stay two-step** (`cli/cmd_account.go:62-70`): a second signup POST mints a new code and invalidates the one being typed. `login` is that same flow under a second name — the server answers `/v1/auth/signup` identically for a known and an unknown address (no enumeration) and `/v1/auth/verify` mints the token either way — so both commands share `newAuthCmd`. Either one saves the sign-in as a switchable account.

## Key Directories

| Path | Purpose |
|---|---|
| `cmd/domain-cli/` | `main` package; 7-line entry point, `var version = "dev"` |
| `apitypes/` | **Cross-module wire contract.** Imported by both this CLI and the private server |
| `cli/` | Single flat package: root wiring, client, config, flows, all commands |
| `cli/i18n/` | Bundle init, locale detection/matching, `T()` |
| `cli/locales/` | 13 flat JSON bundles + `//go:embed *.json` |
| `skills/domain-cli/` | Agent-facing usage contract (`SKILL.md`) |
| `.github/workflows/` | `ci.yml` (build/vet/test), `release.yml` (5-target tag release) |

Commands are grouped thematically, **not** one file per command: `cmd_account.go` (signup/login/logout/whoami/account), `cmd_order.go` (register/renew/transfer/orders/invoice), `cmd_domains.go` (legal/domains/dns), `cmd_contacts.go` (contacts/billing), `cmd_search.go`.

## Development Commands

If a `go.work` file exists one level above the repo (local developer setups that also mount other local modules), `go env GOWORK` is non-empty there. **Prefix local `go` commands with `GOWORK=off`** — otherwise you build a dependency graph CI does not have.

```sh
# Exact CI gate (.github/workflows/ci.yml:19-22)
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...

# Local binary (matches .gitignore's /domain-cli)
GOWORK=off go build -o domain-cli ./cmd/domain-cli && ./domain-cli --version

# Release-identical build; the ldflags target is exactly `main.version`
GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=v0.0.0-local" \
  -o dist/domain-cli-linux-amd64 ./cmd/domain-cli

# Smoke a flow against a local API
DOMAIN_CLI_SERVER_URL=http://localhost:8080 ./domain-cli search mybrand
LANG=es_ES.UTF-8 ./domain-cli --help     # section headers must be Spanish too
```

**There is no lint step, no formatter gate, no Makefile, no `.golangci.yml`.** `go vet ./...` is the entire static-analysis gate. `gofmt -l .` is worth running but is not enforced — do not add a linter config as a drive-by.

## Code Conventions & Common Patterns

**Command registration — constructor + explicit `AddCommand`, never `init()`:**
```go
// cli/root.go:110-113
root.AddCommand(newSignupCmd(app))
root.AddCommand(newSearchCmd(app))
```
Each `func newXxxCmd(app *App) *cobra.Command` attaches its flags before returning.

**Standard `RunE` body — auth gate → client call → `fail()` → output:**
```go
c, err := app.requireClient()
if err != nil { return err }
resp, err := c.Search(args[0])
if err != nil { return fail(err) }
```
`fail()` is mandatory on client errors: it localizes 429 and passes `*APIError.Message` through **verbatim** (`cli/root.go:35-44`). Never wrap a server message in `i18n.T` — verbatim upstream text is the designed channel.

**All user-facing text goes through `i18n.T(key, "Name", value, ...)`.** No literal user-visible string exists outside `cli/locales/*.json` (exceptions: flag help text and five `%w`-wrapped infra errors in `client.go`/`config.go`). Message IDs are dot-separated `domain.action[.qualifier]`: `order.created`, `orders.hdr.status`, `err.ratelimited`, `tmpl.usage`, `*.short`/`*.long`. `T` returns the **key itself** on a miss, so typos print as `order.creatd` rather than failing.

**Locale is resolved in `i18n.init()`, before the command tree is built** — Cobra captures `Short`/`Long` at construction (`cli/root.go:46-48`). Cobra's own English scaffolding is replaced via `localizedUsageTemplate()` placeholder substitution, not field translation. Use the localized arg validators `exactArgs/minArgs/rangeArgs`, never raw `cobra.ExactArgs`.

**Non-interactive enforcement — three-way switch, never a hang:**
```go
switch {
case rf.any():           // build from flags; error names EVERY missing flag
case app.p.stdinIsTTY(): // prompt
default:                 return missingFlags("--first-name", "--email", ...)
}
```
TTY detection is on the prompter's reader (Cobra's `In`), **not** `os.Stdin` (`cli/noninteractive.go:31-51`) — so piped input is treated as an agent, and piping `y\n` does **not** grant consent. It asks the kernel (`term.IsTerminal`), not the file mode, so `/dev/null` is not mistaken for a terminal. Always prompt through `app.p`; never build a second `bufio.Reader` over the input stream (one shared reader per run, `cli/prompt.go:21-26`).

`requireYes` gates every consequential command: order placement, `domains autorenew on`, `account delete`, `domains unlock`, `domains authcode`. `domains lock` deliberately has no gate — it only removes transferability.

**Config precedence is centralized** (`cli/config.go:147-178`): `--server` > `DOMAIN_CLI_SERVER_URL` > config file > (interactive prompt | `missingFlags("--server")`); `--token` > `--token-file` > `DOMAIN_CLI_TOKEN` > config file. `DOMAIN_CLI_ENV=dev` only flips the first-run *default* to `http://localhost:8080`.

**The resolved server is validated and the stored token is bound to it.** `ValidateServerURL` rejects anything but https (plain http only for a loopback host) — the token travels in the `Authorization` header of every request, so a host named by `--server`/`DOMAIN_CLI_SERVER_URL` must not be able to downgrade it to cleartext. `tokenBelongsToServer` refuses to send a config-file token to a server other than the one recorded beside it; an explicit `--token`/`DOMAIN_CLI_TOKEN` is taken as the caller's deliberate choice. Both live in `cli/config.go`.

**Output**: always `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` — direct `os.Stdout` appears once, in `stdoutIsTTY()`. Tables via `printTable` + `text/tabwriter`; booleans via `yesNo`; money via `formatMoney(cents, currency)`. `emitJSON` is a no-op unless `--json`, so call it unconditionally.

**Test seams are package-level vars, not interfaces** — `var userConfigDir = os.UserConfigDir`, `var pollInterval/pollTimeout`. New sleeps/OS lookups follow that pattern with a `// indirected for tests` comment.

**Hard invariants**
- `apitypes/` **must stay at the module root**, never under `internal/`: the private server imports it across modules. Any field/tag change is a two-repo breaking change, only consumable after a `v*` tag here.
- Wire JSON is **camelCase without exception**; `omitempty` only on genuinely optional fields (so `locked: false` still serializes).
- `config.json` uses **snake_case** (`server_url`, `token`, `accounts[].email`) while the wire is camelCase. Two conventions in one package on purpose — do not harmonize.
- Token is written only to a `0600` file inside a `0700` dir, and to the `Authorization` header. Both modes are re-asserted on every `Save` (a pre-existing lax mode is repaired, not inherited) and the write is atomic via temp-file + rename, so a planted `config.json` symlink is replaced instead of followed. `Load` never repairs; `warnLaxConfigPerms` prints a localized warning when an existing file is group/world-readable. There is no logging framework; nothing logs tokens, 6-digit codes or domain auth codes. Deliberate exception: `signup --json` and `login --json` emit the token (they share one implementation).
- Go files carry a package doc comment and **no SPDX/license header**.
- No source file uses `context.Context` for HTTP; adding it means changing ~25 client signatures at once.

## Important Files

| File | Why it matters |
|---|---|
| `cmd/domain-cli/main.go:14` | `var version = "dev"`; the `-X main.version=` target |
| `cli/root.go:58-98` | Config/client resolution + the whole command tree |
| `cli/root.go:100-104` | The **only five** global flags: `--server`, `--token`, `--token-file`, `--json`, `--yes` (no `--lang`) |
| `cli/client.go:60-121` | `do()` + `decodeError()` — headers and the 429/`*APIError` split |
| `cli/config.go:147-178` | Env var names and precedence; server validation and token binding |
| `cli/noninteractive.go` | `missingFlags`, `requireYes`, `stdinIsTTY`, `emitJSON` — the agent contract |
| `cli/orderflow.go` | Quote→confirm→order→checkout→poll; registrant precedence |
| `cli/i18n/i18n.go:26-33` | `Supported` + `supportedTags`, index-aligned with `cli/locales/*.json` |
| `apitypes/apitypes.go:1-7` | The contract invariants, stated in the package doc |
| `install.sh` | Served publicly from `main`; consumes release asset names verbatim |
| `skills/domain-cli/SKILL.md` | Authored agent rules (human approval, no payment automation) |

Env vars in play: `DOMAIN_CLI_SERVER_URL`, `DOMAIN_CLI_TOKEN`, `DOMAIN_CLI_ENV`, `LC_ALL`/`LC_MESSAGES`/`LANG`, `SSH_CONNECTION`/`SSH_TTY` (suppress browser opening), plus `GITHUB_REPO`/`INSTALL_DIR` and optional `GH_TOKEN`/`GITHUB_TOKEN` for private-repo installs. With no token, `install.sh` uses an authenticated `gh` login when available before falling back to public URLs.

## Runtime/Tooling Preferences

- **Go 1.27**, pinned in three places that must move together: `go.mod:3`, `ci.yml`, `release.yml`. There is no `toolchain` directive — do not let a tool add one.
- Dependencies are deliberately minimal: `spf13/cobra`, `nicksnyder/go-i18n/v2`, `golang.org/x/text`, `golang.org/x/term` (kernel-backed terminal detection — `ModeCharDevice` is true for `/dev/null`, which is not a terminal). Release builds are `CGO_ENABLED=0 -trimpath -s -w` across `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64`, so **no cgo dependency may be added**.
- Release artifacts are `dist/domain-cli-<goos>-<goarch>` (+ `.exe`/`.zip` for Windows), each paired with its own `<asset>.sha256`. Per-asset checksums avoid matrix jobs racing to overwrite one aggregate file. `install.sh` fetches the platform binary and matching `.sha256`; keep those names synchronized with `.github/workflows/release.yml`.
- `install.sh` is POSIX `/bin/sh` with `set -eu` (no `pipefail`, no bashisms, tab-indented). It has **no `VERSION` pin** and Windows is intentionally rejected with a pointer to the landing page.
- No project-wide planning documents are tracked in this repo: where local, untracked notes disagree with repo files, **the repo wins**.

## Testing & QA

Stdlib `testing` only — no testify, no `cmp`, no golden files, no `testdata/`. Tests are internal (`package cli`, `package i18n`) and reach unexported identifiers. Test names are behaviour claims (`TestRegisterNonInteractiveMissingFlags`); table cases are lowercase rules (`"env beats config"`).

```sh
GOWORK=off go test ./...
GOWORK=off go test ./cli/ -run TestOrderPollTransitions -v
GOWORK=off go test ./cli/i18n/ -run TestLocaleParity -count=1   # after ANY locale edit
```

No coverage target, no `-race`, no `-shuffle` in CI — do not claim CI runs them.

**Canonical command test** (`cli/commands_test.go:27-39`) — reuse the cross-file helpers `runRoot`, `setLocale`, `setConfigDir`, `fakeServer` rather than redeclaring them:
```go
root := NewRootCmd("test")
root.SetOut(&out); root.SetErr(&out)        // stdout+stderr merged
root.SetIn(strings.NewReader(stdin))        // non-*os.File ⇒ stdinIsTTY() == false
root.SetArgs(args); root.Execute()
```
Standard prologue: `setConfigDir(t)` (else you write the developer's real `~/.config/domain-cli/config.json`), `setLocale(t, "en")` (else assertions fail on a `LANG=es` machine), and `t.Setenv("SSH_CONNECTION", "1")` for any flow returning a `checkoutURL` (else `go test` may spawn a real browser).

- `runRoot` `t.Fatal`s on error — **error-path tests must inline the root block** and assert on the flag name in the output (`--auth-code`, `--yes`).
- Fake servers are one `http.HandlerFunc` with a path switch whose `default:` arm calls `t.Errorf("unexpected request …")`. Responses are built from real `apitypes` structs and `json.NewEncoder`, never hand-written JSON — that is what keeps the cross-repo contract honest. Adding a preflight call to an existing flow breaks unrelated handlers; add the case, don't relax the default.
- **Never add `t.Parallel()`**: the suite relies on package globals (`serverFlag`, `jsonFlag`, `yesFlag`, `pollInterval`, `userConfigDir`, `i18n.current`) and heavy `t.Setenv`, which panics under parallel tests.
- Handler-goroutine flags must use `sync/atomic` (`atomic.Bool`, `atomic.Int32`).

**Locale contract — `TestLocaleParity` (`cli/i18n/i18n_test.go:83-134`).** Adding one user-facing string means, in the same commit: add the key to `en.json` **and translate it into all 12 other bundles with the identical `{{.Placeholder}}` set**. The test enforces flat `map[string]string` JSON (nested go-i18n plural forms hard-fail), keyset parity in **both** directions, per-message placeholder parity, and file count == `len(i18n.Supported)`. A new locale needs the JSON file **plus** entries in `Supported` and `supportedTags`; a `Supported` entry without a file **panics in `init()`**. Note some Spanish strings are asserted in `cli/root_test.go`, so editing `es.json` can break `cli` tests too.

## Known Defects — Do Not Mimic or Silently Inherit

- `billing show` (`cli/cmd_contacts.go:254-257`) and `billing set` (`:326-329`) call `emitJSON` **and also** print human lines, so `--json` output is unparseable there. Only `orders` (`cli/cmd_order.go:173-174`) returns early. (`signup`/`login`/`whoami` used to share this defect; the auth and account commands now gate their human lines on `!jsonFlag`.) Agents should pass `--no-wait` with `--json`, since `pollOrder`'s status lines are not gated by `jsonFlag` either.
- `domains autorenew` calls `i18n.T("domains.autorenew.off")` with no args, then appends `"Name", args[0]` to `Fprintln` → renders `… for <no value>. Name acme.example`.
- `pollOrder` returns `nil` on `failed`/`refunded`/`expired`/`cancelled` and on timeout — a failed order still exits 0. Scripts must read the status, not `$?`.
- Client URL building never escapes (`"/v1/domains/"+name`, `fmt.Sprintf("/v1/quote?domain=%s…")`). New endpoints taking free-form input need escaping added deliberately.
- Orphaned locale keys exist (`confirm.prompt`, `signup.codePrompt`, `tmpl.additionalCommands`, `err.missingFlag`) — a key's presence does not mean it is wired.
- `README.md` drift: it documents no env vars. `SKILL.md` documents `--own-contact` and `--no-wait` on `transfer`; neither exists there.
