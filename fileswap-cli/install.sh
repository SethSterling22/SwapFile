#!/bin/sh
# Installs or updates the FileSwap CLI from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
#   curl -fsSL …/install.sh | sh -s -- --version v0.8.0-alpha.1   # a specific version
#
# It picks the package for this system — .deb (Debian, Ubuntu…), Arch
# package (pacman), or the plain binary in /usr/local/bin elsewhere —,
# checks it against the release's SHA256SUMS, installs it with sudo when
# needed, deletes the download, and restarts the fileswap user service if
# it is running. Re-run it to update.
set -eu

REPO="SethSterling22/SwapFile"
VERSION="latest"

while [ $# -gt 0 ]; do
	case "$1" in
	--version) VERSION="${2:?--version needs a tag, e.g. v0.8.0-alpha.1}"; shift 2 ;;
	--version=*) VERSION="${1#--version=}"; shift ;;
	-h | --help) echo "usage: install.sh [--version <tag>]   (default: the newest release)"; exit 0 ;;
	*) echo "install.sh: unknown option $1" >&2; exit 2 ;;
	esac
done

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

if have curl; then
	fetch() { curl -fsSL "$1"; }
	download() { curl -fsSL -o "$2" "$1"; }
elif have wget; then
	fetch() { wget -qO- "$1"; }
	download() { wget -qO "$2" "$1"; }
else
	die "needs curl or wget"
fi

case "$(uname -m)" in
x86_64 | amd64) DEB_ARCH=amd64 PAC_ARCH=x86_64 ;;
aarch64 | arm64) DEB_ARCH=arm64 PAC_ARCH=aarch64 ;;
*) die "unsupported architecture $(uname -m) (amd64 and arm64 are available)" ;;
esac
[ "$(uname -s)" = Linux ] || die "the FileSwap CLI is for Linux"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
	have sudo || die "needs root or sudo to install"
	SUDO="sudo"
fi

if have apt-get && have dpkg; then
	KIND=deb PATTERN="_${DEB_ARCH}\\.deb\$"
elif have pacman; then
	KIND=pacman PATTERN="-${PAC_ARCH}\\.pkg\\.tar\\.zst\$"
else
	KIND=binary PATTERN="_linux_${DEB_ARCH}\\.tar\\.gz\$"
fi

# "latest" is the newest release, pre-releases included
if [ "$VERSION" = latest ]; then
	API="https://api.github.com/repos/$REPO/releases?per_page=1"
else
	API="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
fi
JSON=$(fetch "$API") || die "could not reach GitHub ($API)"
TAG=$(printf '%s' "$JSON" | grep -o '"tag_name": *"[^"]*"' | head -n1 | cut -d'"' -f4)
URLS=$(printf '%s' "$JSON" | grep -o '"browser_download_url": *"[^"]*"' | cut -d'"' -f4)
[ -n "$TAG" ] || die "release $VERSION not found"
URL=$(printf '%s\n' "$URLS" | grep -E -e "$PATTERN" | head -n1)
SUMS=$(printf '%s\n' "$URLS" | grep -E '/SHA256SUMS$' | head -n1)
[ -n "$URL" ] || die "release $TAG has no $KIND package for $DEB_ARCH"
FILE=${URL##*/}

# A world-readable temporary folder: apt reads local packages as the
# unprivileged _apt user. Removed on exit, whatever happens.
TMP=$(mktemp -d /tmp/fileswap-install.XXXXXX)
trap 'rm -rf "$TMP"' EXIT INT TERM
chmod 755 "$TMP"

say "FileSwap $TAG — $FILE"
download "$URL" "$TMP/$FILE" || die "download failed: $URL"
chmod 644 "$TMP/$FILE"
if [ -n "$SUMS" ] && have sha256sum; then
	download "$SUMS" "$TMP/SHA256SUMS" || die "download failed: $SUMS"
	# Match by hash: older releases list .deb names with "~", which GitHub renames to "."
	HASH=$(sha256sum "$TMP/$FILE" | cut -d' ' -f1)
	grep -q "^$HASH " "$TMP/SHA256SUMS" || die "checksum mismatch for $FILE"
	say "Checksum OK"
fi

case "$KIND" in
deb) $SUDO apt-get install -y -q --allow-downgrades "$TMP/$FILE" ;;
pacman) $SUDO pacman -U --noconfirm "$TMP/$FILE" ;;
binary)
	tar -xzf "$TMP/$FILE" -C "$TMP"
	$SUDO install -m 755 "$TMP"/fileswap_*/fileswap /usr/local/bin/fileswap
	;;
esac

# The service keeps running the old binary until it is restarted
if [ "$(id -u)" -ne 0 ] && systemctl --user is-active --quiet fileswap 2>/dev/null; then
	systemctl --user restart fileswap && say "Restarted the fileswap service"
fi

say ""
say "Installed $(fileswap --version 2>/dev/null || echo "fileswap $TAG")."
say "Next: fileswap init --name <name> --server <host>:7341   (once), then: fileswap start"
