#!/usr/bin/env bash
# Installs orOS from the ISO onto an empty disk image with oros-system (as
# the System button does from a stick), then boots that disk alone with
# UEFI Secure Boot and checks that orOS runs and the copied data is there.
#   sudo scripts/test-install.sh out/orOS-USB-x.iso test-out/install
set -euo pipefail

ISO="$1"
OUT="${2:-test-out/install}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/config/includes.chroot/opt/oros/oros-system"
mkdir -p "$OUT"

MNT="$(mktemp -d)"
mount -o loop,ro "$ISO" "$MNT"
# A home folder like the stick's, with a file that must survive the move.
HOMES="$(mktemp -d)"
mkdir -p "$HOMES/oros/oros/data"
echo kept > "$HOMES/oros/oros/data/install-marker"
chown -R 1000:1000 "$HOMES/oros"

rm -f "$OUT/disk.img"
truncate -s 10G "$OUT/disk.img"
"$BIN" install --disk "$OUT/disk.img" --medium "$MNT" --home "$HOMES/oros" --progress "$OUT/install.json" --yes
umount "$MNT"
sfdisk -l "$OUT/disk.img"

DISK="$OUT/disk.img" MARKER=install-marker "$ROOT/scripts/test-boot.sh" "$ISO" "$OUT"
rm -f "$OUT/disk.img"
