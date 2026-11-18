#!/bin/sh
# install.sh — install the domain-cli binary from the latest GitHub release.
#
# Usage: curl -fsSL https://registerdomaincli.com/install.sh | sh
#
# Environment:
#   GITHUB_REPO   repository to download from (default registerdomaincli/cli)
#   INSTALL_DIR   installation directory (default ~/.local/bin)
#
# The release is public: the script downloads it anonymously and never reads
# a GitHub token, a `gh` login or any other credential from the machine.
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

echo "Downloading $ASSET from $REPO latest release..."

BASE="https://github.com/$REPO/releases/latest/download"
FETCH_OK=0
# Anonymous means not even the user's own downloader config: a `header =
# "Authorization: …"` line in ~/.curlrc, or credentials in ~/.wgetrc or
# ~/.netrc, would otherwise ride along. curl skips ~/.curlrc only when -q is
# its FIRST argument (and reads ~/.netrc only with -n, which is never
# passed). GNU wget gets --no-config and netrc=off, and one too old to have
# --no-config (before 1.17) is refused; BusyBox wget reads neither file and
# rejects those options, so it gets none.
if command -v curl >/dev/null 2>&1; then
	curl -q -fsSL "$BASE/$ASSET" -o "$TMP/domain-cli" &&
		curl -q -fsSL "$BASE/$ASSET.sha256" -o "$TMP/$ASSET.sha256" && FETCH_OK=1
elif command -v wget >/dev/null 2>&1; then
	# The probes themselves must not load ~/.wgetrc either. Both point WGETRC
	# at /dev/null (an old GNU Wget reads its rc file before rejecting an
	# unknown --no-config), and only a GNU Wget that supports --no-config
	# prints its banner on the first one.
	set --
	if WGETRC=/dev/null wget --no-config --version 2>/dev/null | grep -q "GNU Wget"; then
		set -- --no-config -e netrc=off
	elif WGETRC=/dev/null wget --version 2>/dev/null | grep -q "GNU Wget"; then
		# Fail closed: a GNU wget that cannot skip ~/.wgetrc (older than
		# 1.17) could send whatever credentials or headers it configures.
		echo "error: this wget cannot ignore ~/.wgetrc (needs GNU Wget 1.17+)." >&2
		echo "Install curl, or a newer wget, and re-run." >&2
		exit 1
	fi
	wget "$@" -q "$BASE/$ASSET" -O "$TMP/domain-cli" &&
		wget "$@" -q "$BASE/$ASSET.sha256" -O "$TMP/$ASSET.sha256" && FETCH_OK=1
else
	echo "error: need curl or wget to download the release." >&2
	exit 1
fi
if [ "$FETCH_OK" -ne 1 ]; then
	echo "error: could not download $ASSET from $REPO." >&2
	echo "Check your connection, or that the latest release has a $GOOS/$GOARCH build." >&2
	exit 1
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
