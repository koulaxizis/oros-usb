#!/usr/bin/env bash
# Puts the image's own system onto a stick image's persistence partition
# with "oros-system update" (slot A, as a monthly update would), then boots
# the stick and checks that the system from slot A started and orOS ran.
#   sudo scripts/test-update.sh out/orOS-USB-x.iso test-out/update
set -euo pipefail

ISO="$1" OUT="${2:-test-out/update}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/config/includes.chroot/opt/oros/oros-system"
mkdir -p "$OUT"

"$ROOT/scripts/update-assets.sh" "$ISO" "$OUT/release" 9.9.9-test

IMG="$OUT/usb.img"
rm -f "$IMG"
truncate -s 8G "$IMG"
dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
OROS_YES=1 "$ROOT/scripts/add-persistence.sh" "$IMG"

# The options the stick's menu starts the live system with.
ARGS="$(xorriso -osirrox on -indev "$ISO" -extract /boot/grub/grub.cfg /dev/stdout 2>/dev/null |
	awk '$1 == "linux" { $1 = ""; $2 = ""; print; exit }')"
echo "kernel options: $ARGS"

read -r START SECTORS < <(partx -g -o START,SECTORS -n -1 "$IMG")
LOOP="$(losetup -f --show -o $((START * 512)) --sizelimit $((SECTORS * 512)) "$IMG")"
MNT="$(mktemp -d)"
mount "$LOOP" "$MNT"
"$BIN" update --yes --source "$OUT/release" --target "$MNT" --cmdline "$ARGS" --progress "$OUT/progress.json"
cat "$MNT/oros-system/boot.cfg"
ls -la "$MNT/oros-system/A"
umount "$MNT"
losetup -d "$LOOP"
rm -rf "$OUT/release"

DISK="$IMG" "$ROOT/scripts/test-boot.sh" "$ISO" "$OUT"
LOOP="$(losetup -f --show -o $((START * 512)) --sizelimit $((SECTORS * 512)) "$IMG")"
mount -o ro "$LOOP" "$MNT"
SLOT="$(grep -a -h 'slot=' "$(find "$MNT" -name oros-system.log | head -1)" | tail -1 || true)"
umount "$MNT"
losetup -d "$LOOP"
echo "oros-system.log: $SLOT"
if echo "$SLOT" | grep -q 'slot="A"'; then
	echo "PASS: the updated system (slot A on the persistence partition) started"
else
	echo "FAIL: the stick's own system started, not the update" >&2
	grep -a -m3 -i "command line" "$OUT/serial.log" >&2 || true
	exit 1
fi
rm -f "$IMG"
