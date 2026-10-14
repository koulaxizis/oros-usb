#!/usr/bin/env bash
# Builds the orOS USB image. Run as root on Debian 13 (or in a privileged
# debian:trixie container) with live-build, golang-go and git installed.
#   scripts/build.sh [version]   ->  out/orOS-USB-<version>.iso
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
OPT="$ROOT/config/includes.chroot/opt/oros"
cd "$ROOT"

echo "== orOS web app"
git clone -q --depth 1 https://github.com/koulaxizis/oros "$WORK/oros"
rm -rf "$OPT" && mkdir -p "$OPT/app"
(cd "$WORK/oros" && tar --exclude=./.git --exclude=./.github --exclude=./android \
	--exclude=./relay --exclude=./extension --exclude=./tests -cf - .) | tar -xf - -C "$OPT/app"
echo "$VERSION $(git -C "$WORK/oros" rev-parse --short HEAD)" > "$OPT/app/.oros-build"

echo "== launcher from oros-portable"
git clone -q --depth 1 https://github.com/koulaxizis/oros-portable "$WORK/portable"
(cd "$WORK/portable/launcher" && CGO_ENABLED=0 go build -trimpath \
	-ldflags "-s -w -X main.version=usb-$VERSION" -o "$OPT/oros-portable" .)

echo "== live-build"
lb clean --purge
lb config
lb build
mkdir -p out
mv live-image-amd64.hybrid.iso "out/orOS-USB-$VERSION.iso"
rm -rf "$WORK"
ls -lh out/
