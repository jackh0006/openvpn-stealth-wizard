#!/bin/bash
# openvpn-stealth-wizard one-line installer.
# Usage: curl -sSL https://raw.githubusercontent.com/jackh0006/openvpn-stealth-wizard/main/install.sh | sudo bash
# One command: it explains every step, then proves it worked.
set -u
REPO="jackh0006/openvpn-stealth-wizard"
DEST="/usr/local/bin/wizard"

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
good() { printf '\033[1;32m  ✔ %s\033[0m\n' "$*"; }
die()  { printf '\033[1;31m  ✘ %s\033[0m\n' "$*" >&2; exit 1; }

say "Step 1/4: figuring out your machine..."
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   GOARCH="amd64" ;;
  aarch64|arm64)  GOARCH="arm64" ;;
  *) die "sorry, I only know amd64 and arm64 machines (yours: $ARCH)" ;;
esac
good "you have a $ARCH machine, I will fetch the $GOARCH wizard"

say "Step 2/4: asking GitHub for the newest version..."
command -v curl >/dev/null || die "I need 'curl' first: apt install -y curl"
command -v tar >/dev/null || die "I need 'tar' first: apt install -y tar"
TAG="$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep -m1 '"tag_name"' | cut -d'"' -f4)"
[ -n "$TAG" ] || die "GitHub did not answer (no internet?)"
good "newest version is $TAG"

say "Step 3/4: downloading and installing to $DEST..."
TMP="$(mktemp -d)" || die "cannot make temp dir"
trap 'rm -rf "$TMP"' EXIT
URL="https://github.com/$REPO/releases/download/$TAG/openvpn-stealth-wizard_${TAG#v}_linux_${GOARCH}.tar.gz"
curl -sSL -o "$TMP/wizard.tar.gz" "$URL" || die "download failed from $URL"
tar xzf "$TMP/wizard.tar.gz" -C "$TMP" || die "that file was not a program (download broken?)"
BIN="$(find "$TMP" -maxdepth 2 -name 'wizard*' -type f | head -1)"
[ -n "$BIN" ] || die "no wizard program inside the download"
install -m 0755 "$BIN" "$DEST" || die "cannot write to $DEST (run me with sudo?)"
good "installed $DEST"

say "Step 4/4: proving it works..."
"$DEST" --version || die "installed but will not start"
good "all done! Next step:"
echo "    sudo wizard          (guided setup with explanations)"
echo "    wizard --help        (all commands with examples)"
