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

echo "== system bridge and extension"
(cd system && CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$OPT/oros-system" .)
rm -rf "$OPT/system-ext" && cp -a system/extension "$OPT/system-ext"
echo "$VERSION" > "$OPT/version"

echo "== boot menus: start orOS by itself after 3 seconds"
# live-build's menus wait for a key forever; take its own templates and set
# a timeout (GRUB for UEFI, isolinux for BIOS).
rm -rf config/bootloaders && mkdir -p config/bootloaders
cp -a /usr/share/live/build/bootloaders/grub-pc /usr/share/live/build/bootloaders/isolinux config/bootloaders/
printf '\nset default=0\nset timeout=3\n' >> config/bootloaders/grub-pc/config.cfg
sed -i 's/^timeout .*/timeout 30/' config/bootloaders/isolinux/isolinux.cfg
grep -q '^timeout 30' config/bootloaders/isolinux/isolinux.cfg
# System updates (oros-system update) put a newer system on the persistence
# partition with its own menu entries in /oros-system/boot.cfg; they come
# first. UEFI only (isolinux cannot read ext4); the stick's own system
# stays in the menu below them.
cat >> config/bootloaders/grub-pc/config.cfg <<'GRUB'

if regexp --set=1:orosdev '^\(?([^,)]+)' "$root"; then
	search --no-floppy --file --set=orosupd --hint="${orosdev}," /oros-system/boot.cfg
else
	search --no-floppy --file --set=orosupd /oros-system/boot.cfg
fi
if [ -n "$orosupd" ]; then
	source ($orosupd)/oros-system/boot.cfg
fi
GRUB
# orOS splash instead of Debian's (640x480 for both menus).
find config/bootloaders -name 'splash.svg' -delete
cp assets/splash.png config/bootloaders/grub-pc/splash.png
cp assets/splash.png config/bootloaders/isolinux/splash.png

echo "== live-build"
lb clean --purge
lb config
lb build
mkdir -p out
mv live-image-amd64.hybrid.iso "out/orOS-USB-$VERSION.iso"
rm -rf "$WORK"
ls -lh out/
