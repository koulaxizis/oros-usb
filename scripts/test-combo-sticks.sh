#!/usr/bin/env bash
# Writes a combo stick (orOS USB image + orOS Portable on NTFS) to a disk
# image with make-combo-sticks.sh, then boots it like test-boot.sh does.
#   sudo scripts/test-combo-sticks.sh out/orOS-USB-x.iso orOS-Portable.zip test-out/combo
set -euo pipefail

ISO="$1" PORTABLE="$2" OUT="${3:-test-out/combo}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$OUT"
rm -f "$OUT/stick.img"
truncate -s 12G "$OUT/stick.img"
DEV="$(losetup -f --show "$OUT/stick.img")"
OROS_STICKS_ALLOW_ANY=1 "$ROOT/scripts/make-combo-sticks.sh" --yes "$ISO" "$PORTABLE" "$DEV"
losetup -d "$DEV"

DISK="$OUT/stick.img" "$ROOT/scripts/test-boot.sh" "$ISO" "$OUT"
rm -f "$OUT/stick.img"
