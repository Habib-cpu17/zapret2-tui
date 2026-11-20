#!/bin/sh
# One-line installer for zapret2-tui:
#   curl -fsSL https://raw.githubusercontent.com/Habib-cpu17/zapret2-tui/master/install.sh | sh
# Downloads the zapret2-tui binary for this CPU from the fork's GitHub releases,
# verifies its sha256, installs it to /usr/local/bin and starts it.
set -eu

REPO="${ZAPRET_TUI_REPO:-Habib-cpu17/zapret2-tui}"
DEST="${ZAPRET_TUI_DEST:-/usr/local/bin}"

die() { echo "error: $*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || die "only Linux is supported"

case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	armv7*|armv8l) ARCH=armv7 ;;
	*) die "unsupported CPU: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
else
	die "need curl or wget"
fi

SUDO=
if [ "$(id -u)" != 0 ]; then
	command -v sudo >/dev/null 2>&1 || die "run as root or install sudo"
	SUDO=sudo
fi

BIN="zapret2-tui-linux-$ARCH"
URL="https://github.com/$REPO/releases/latest/download"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "* downloading $BIN from github.com/$REPO"
fetch "$URL/$BIN" "$TMP/$BIN" || die "download failed"
fetch "$URL/$BIN.sha256" "$TMP/$BIN.sha256" || die "checksum download failed"

echo "* verifying sha256"
WANT="$(cut -d' ' -f1 < "$TMP/$BIN.sha256")"
GOT="$(sha256sum "$TMP/$BIN" | cut -d' ' -f1)"
[ "$WANT" = "$GOT" ] || die "checksum mismatch"

echo "* installing to $DEST/zapret2-tui"
$SUDO install -m 0755 "$TMP/$BIN" "$DEST/zapret2-tui"

echo
echo "Installed. Run:  sudo zapret2-tui"
# when piped from curl, stdin is the script; reattach the terminal for the UI
if [ -t 1 ] && [ -r /dev/tty ]; then
	exec $SUDO "$DEST/zapret2-tui" </dev/tty
fi
