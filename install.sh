#!/bin/sh
# install.sh — install the domain-cli binary from the latest GitHub release.
#
# Usage: curl -fsSL https://registerdomaincli.com/install.sh | sh
#
# Environment:
#   GITHUB_REPO   repository to download from (default registerdomaincli/cli)
#   INSTALL_DIR   installation directory (default ~/.local/bin)
#   GH_TOKEN      GitHub token, required only while the repository is private
#                 (GITHUB_TOKEN is honoured too, and an authenticated `gh`
#                 CLI is used automatically when neither is set)
#
# Supported platforms: linux/darwin × amd64/arm64. Windows users should use
# the PowerShell block on the landing page instead — this is a POSIX sh
# script that Windows cannot run natively.
set -eu

REPO="${GITHUB_REPO:-registerdomaincli/cli}"
OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
	Linux) GOOS=linux ;;
	Darwin) GOOS=darwin ;;
	MINGW*|MSYS*|CYGWIN*|Windows*)
		echo "error: this installer does not run on Windows." >&2
		echo "Use the PowerShell block on the landing page:" >&2
		echo "https://registerdomaincli.com" >&2
		exit 1
		;;
	*)
		echo "error: unsupported operating system: $OS" >&2
		echo "Supported: linux, darwin. Windows: see https://registerdomaincli.com" >&2
		exit 1
		;;
esac

case "$ARCH" in
	x86_64|amd64) GOARCH=amd64 ;;
	aarch64|arm64) GOARCH=arm64 ;;
	*)
		echo "error: unsupported architecture: $ARCH" >&2
		echo "Supported: amd64 (x86_64), arm64 (aarch64)." >&2
		exit 1
		;;
esac

ASSET="domain-cli-$GOOS-$GOARCH"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# The release assets are public once the repo is, but while it is PRIVATE the
# unauthenticated URLs 404 for everyone. Pick the strongest credential
# available, in order:
#   1. GH_TOKEN / GITHUB_TOKEN -> GitHub API with a bearer token (CI, servers)
#   2. an authenticated gh CLI -> the operator's own GitHub login
#   3. nothing                 -> the public unauthenticated URLs
TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ] && command -v gh >/dev/null 2>&1; then
	TOKEN=$(gh auth token 2>/dev/null || true)
fi

echo "Downloading $ASSET from $REPO latest release..."

if [ -n "$TOKEN" ]; then
	# Private-repo capable path. A release asset can only be fetched through
	# the API (the browser download URL ignores the token), and the API needs
	# the asset's numeric id, so resolve it from the latest release.
	command -v curl >/dev/null 2>&1 || {
		echo "error: authenticated download needs curl." >&2
		exit 1
	}
	API="https://api.github.com/repos/$REPO/releases/latest"
	REL=$(curl -fsSL -H "Authorization: Bearer $TOKEN" \
		-H "Accept: application/vnd.github+json" "$API") || {
		echo "error: cannot read the latest release of $REPO." >&2
		echo "Check that your token or gh login can read that repository." >&2
		exit 1
	}
	# GitHub returns pretty-printed JSON. Keep the most recent top-level `id`
	# while scanning and emit it when the following asset `name` matches.
	# The prior grep pipeline worked line-by-line, so the name line never also
	# contained its id and every authenticated install reported "no asset".
	asset_id() {
		printf '%s\n' "$REL" | awk -v target="$1" '
			/^[[:space:]]*"id":[[:space:]]*[0-9]+,/ {
				id = $0
				sub(/^[^:]*:[[:space:]]*/, "", id)
				sub(/,.*/, "", id)
			}
			index($0, "\"name\": \"" target "\"") { print id; exit }
		'
	}
	# An absent asset makes awk print nothing without tripping `set -e`; the
	# explicit guard below can therefore return the useful error.
	BIN_ID=$(asset_id "$ASSET")
	SUM_ID=$(asset_id "$ASSET.sha256")
	if [ -z "$BIN_ID" ] || [ -z "$SUM_ID" ]; then
		echo "error: the latest release of $REPO has no $ASSET asset." >&2
		echo "Available builds may not cover $GOOS/$GOARCH yet." >&2
		exit 1
	fi
	AURL="https://api.github.com/repos/$REPO/releases/assets"
	curl -fsSL -H "Authorization: Bearer $TOKEN" \
		-H "Accept: application/octet-stream" "$AURL/$BIN_ID" -o "$TMP/domain-cli"
	curl -fsSL -H "Authorization: Bearer $TOKEN" \
		-H "Accept: application/octet-stream" "$AURL/$SUM_ID" -o "$TMP/$ASSET.sha256"
else
	BASE="https://github.com/$REPO/releases/latest/download"
	if command -v curl >/dev/null 2>&1; then
		FETCH_OK=0
		curl -fsSL "$BASE/$ASSET" -o "$TMP/domain-cli" &&
			curl -fsSL "$BASE/$ASSET.sha256" -o "$TMP/$ASSET.sha256" && FETCH_OK=1
	elif command -v wget >/dev/null 2>&1; then
		FETCH_OK=0
		wget -q "$BASE/$ASSET" -O "$TMP/domain-cli" &&
			wget -q "$BASE/$ASSET.sha256" -O "$TMP/$ASSET.sha256" && FETCH_OK=1
	else
		echo "error: need curl or wget to download the release." >&2
		exit 1
	fi
	if [ "$FETCH_OK" -ne 1 ]; then
		echo "error: could not download $ASSET from $REPO." >&2
		echo "If that repository is private, authenticate first:" >&2
		echo "  gh auth login                 # then re-run this script" >&2
		echo "  GH_TOKEN=<token> sh install.sh # or pass a token directly" >&2
		exit 1
	fi
fi

if command -v sha256sum >/dev/null 2>&1; then
	GOT=$(sha256sum "$TMP/domain-cli" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	GOT=$(shasum -a 256 "$TMP/domain-cli" | cut -d' ' -f1)
else
	echo "error: need sha256sum or shasum to verify the download." >&2
	exit 1
fi

EXPECTED=$(cut -d' ' -f1 <"$TMP/$ASSET.sha256")
if [ -z "$EXPECTED" ]; then
	echo "error: no checksum published for $ASSET." >&2
	exit 1
fi
if [ "$GOT" != "$EXPECTED" ]; then
	echo "error: checksum mismatch for $ASSET." >&2
	echo "expected: $EXPECTED" >&2
	echo "got:      $GOT" >&2
	exit 1
fi

# Preferred location: ~/.local/bin (no root needed). Fall back to
# /usr/local/bin with a sudo prompt when that is not on PATH.
PREFIX="${INSTALL_DIR:-$HOME/.local/bin}"
if mkdir -p "$PREFIX" 2>/dev/null; then
	DEST="$PREFIX"
else
	echo "cannot create $PREFIX; falling back to /usr/local/bin" >&2
	DEST="/usr/local/bin"
	if ! [ -w "$DEST" ]; then
		echo "error: need root to install into $DEST; re-run with sudo." >&2
		exit 1
	fi
fi

chmod +x "$TMP/domain-cli"
mv "$TMP/domain-cli" "$DEST/domain-cli"

echo "Installed $(command -v domain-cli || echo "$DEST/domain-cli")"
case ":$PATH:" in
	*":$DEST:"*) ;;
	*)
		echo "note: $DEST is not on your PATH." >&2
		echo "Add this to your shell profile: export PATH=\"$DEST:\$PATH\"" >&2
		;;
esac
