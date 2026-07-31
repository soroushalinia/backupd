#!/bin/sh
# backupd installer - fetches the latest release from GitHub and installs
# the binary into ~/.local/bin (like rustup's ~/.cargo/bin), verifying the
# SHA-256 checksum against the published checksums.txt. POSIX sh only;
# requires curl and tar. No root or sudo is ever needed.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | sh
#
# Environment:
#   PREFIX   install directory (default ~/.local/bin)
#   VERSION  release tag to install, e.g. v0.2.0 (default: latest)
set -eu

PREFIX="${PREFIX:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { say "error: $*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
	linux|darwin|freebsd) ;;
	*) die "unsupported operating system: $OS" ;;
esac

MACH="$(uname -m)"
case "$MACH" in
	x86_64|amd64)          ARCH=amd64 ;;
	aarch64|arm64)         ARCH=arm64 ;;
	armv6l)                ARCH=armv6 ;;
	armv7l|armv8l|arm)     ARCH=armv7 ;;
	*) die "unsupported architecture: $MACH" ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ "$VERSION" = "latest" ]; then
	TAG="$(curl -fsSL https://api.github.com/repos/soroushalinia/backupd/releases/latest | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
	[ -n "$TAG" ] || die "could not resolve the latest release"
else
	TAG="$VERSION"
fi

BASE="https://github.com/soroushalinia/backupd/releases/download/$TAG"
TARBALL="backupd_${TAG#v}_${OS}_${ARCH}.tar.gz"

say "installing backupd $TAG ($OS/$ARCH) into $PREFIX"

curl -fsSL "$BASE/$TARBALL" -o "$TMP/$TARBALL" || die "downloading $TARBALL (does $TAG support $OS/$ARCH?)"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt" || die "downloading checksums.txt"

SHA="$(awk -v f="$TARBALL" '$2 == f {print $1}' "$TMP/checksums.txt")"
[ -n "$SHA" ] || die "no checksum found for $TARBALL"

if command -v sha256sum >/dev/null 2>&1; then
	(printf '%s  %s\n' "$SHA" "$TMP/$TARBALL" | sha256sum -c -) >/dev/null 2>&1 || die "checksum mismatch for $TARBALL"
elif command -v shasum >/dev/null 2>&1; then
	(printf '%s  %s\n' "$SHA" "$TMP/$TARBALL" | shasum -a 256 -c -) >/dev/null 2>&1 || die "checksum mismatch for $TARBALL"
else
	die "neither sha256sum nor shasum is available"
fi

tar -xzf "$TMP/$TARBALL" -C "$TMP" || die "extracting $TARBALL"

mkdir -p "$PREFIX" || die "cannot create $PREFIX"
cp "$TMP/backupd" "$PREFIX/backupd"
chmod +x "$PREFIX/backupd"

say "installed: $PREFIX/backupd"
"$PREFIX/backupd" --version

case ":$PATH:" in
	*":$PREFIX:"*) ;;
	*) say "note: $PREFIX is not on your PATH - add it, e.g. export PATH=\"$PREFIX:\$PATH\"" ;;
esac
