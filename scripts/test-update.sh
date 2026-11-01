#!/usr/bin/env bash
# Puts the image's own system onto a stick image's persistence partition
# with "oros-system update" (slot A, as a monthly update would), then boots
# the stick and checks that the system from slot A started and orOS ran.
# With OROS_TEST_PASSPHRASE the stick's data is encrypted and the slots go
# on its orossys partition; the boot asks for the passphrase (test-boot.sh).
#   sudo scripts/test-update.sh out/orOS-USB-x.iso test-out/update
set -euo pipefail

ISO="$1" OUT="${2:-test-out/update}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/config/includes.chroot/opt/oros/oros-system"
mkdir -p "$OUT"
# shellcheck source=test-data.sh
. "$ROOT/scripts/test-data.sh"
PASS="${OROS_TEST_PASSPHRASE:-}"

"$ROOT/scripts/update-assets.sh" "$ISO" "$OUT/release" 9.9.9-test
# How live-boot picks the persistence partition (printed for diagnosis).
if command -v unmkinitramfs >/dev/null; then
	unmkinitramfs "$OUT/release/initrd.img" "$OUT/initrd" 2>/dev/null || true
	grep -rn -A3 -iE "skip.*(live|medium)|livefs_root|PERSISTENCE_MEDIA|toram" "$OUT/initrd" --include='*misc-helpers*' 2>/dev/null | head -80 || true
	rm -rf "$OUT/initrd"
fi

IMG="$OUT/usb.img"
rm -f "$IMG"
if [ -n "$PASS" ]; then
	test_dm_prepare
	truncate -s 12G "$IMG"
	dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
	OROS_YES=1 OROS_PASSPHRASE="$PASS" OROS_LUKS_TEST=1 "$ROOT/scripts/add-persistence.sh" --encrypt "$IMG"
else
	truncate -s 8G "$IMG"
	dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
	OROS_YES=1 "$ROOT/scripts/add-persistence.sh" "$IMG"
fi

# The options the stick's menu starts the live system with.
xorriso -osirrox on -indev "$ISO" -extract /boot/grub/grub.cfg "$OUT/grub.cfg" >/dev/null 2>&1
ARGS="$(awk '$1 == "linux" { $1 = ""; $2 = ""; print; exit }' "$OUT/grub.cfg")"
[ -n "$ARGS" ] || { echo "no kernel options in the image's grub.cfg" >&2; exit 1; }
echo "kernel options: $ARGS"

data_mount "$IMG"
if [ -n "$PASS" ]; then
	# Encrypted: the slots go on orossys, the partition before the data.
	part_mount "$IMG" 2
	"$BIN" update --yes --source "$OUT/release" --target "$DATA_MNT" --slots "$PART_MNT" --cmdline "$ARGS" --progress "$OUT/progress.json"
	SLOTS="$PART_MNT"
	[ ! -e "$DATA_MNT/oros-system" ] || { echo "FAIL: slots written into the encrypted data" >&2; exit 1; }
else
	"$BIN" update --yes --source "$OUT/release" --target "$DATA_MNT" --cmdline "$ARGS" --progress "$OUT/progress.json"
	SLOTS="$DATA_MNT"
fi
cat "$SLOTS/oros-system/boot.cfg"
ls -la "$SLOTS/oros-system/A"
[ -z "$PASS" ] || part_umount
data_umount
rm -rf "$OUT/release"

DISK="$IMG" "$ROOT/scripts/test-boot.sh" "$ISO" "$OUT"
data_mount "$IMG" ro
SLOT="$(grep -a -h 'slot=' "$(find "$DATA_MNT" -name oros-system.log | head -1)" | tail -1 || true)"
data_umount
echo "oros-system.log: $SLOT"
if echo "$SLOT" | grep -q 'slot="A"'; then
	echo "PASS: the updated system (slot A${PASS:+ on orossys, data encrypted}) started"
else
	echo "FAIL: the stick's own system started, not the update" >&2
	grep -a -m3 -i "command line" "$OUT/serial.log" >&2 || true
	exit 1
fi
rm -f "$IMG"
