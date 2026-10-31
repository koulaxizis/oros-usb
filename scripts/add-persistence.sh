#!/usr/bin/env bash
# Adds a "persistence" partition in the free space after the orOS USB image,
# so data and settings survive reboots. Works on a stick (/dev/sdX) or on a
# disk image file. Windows users can do the same with Rufus instead.
#   sudo scripts/add-persistence.sh [--encrypt] /dev/sdX
# --encrypt keeps the data encrypted with a passphrase (LUKS2), asked on the
# orOS boot screen at every start; see scripts/encrypted-data.sh for the
# layout. The passphrase is asked here twice, or taken from OROS_PASSPHRASE.
set -euo pipefail

ENCRYPT=""
if [ "${1:-}" = "--encrypt" ]; then
	ENCRYPT=1
	shift
fi
TARGET="${1:-}"
if [ -z "$TARGET" ] || { [ ! -b "$TARGET" ] && [ ! -f "$TARGET" ]; }; then
	echo "usage: $0 [--encrypt] /dev/sdX | disk.img" >&2
	exit 1
fi
if [ -n "$ENCRYPT" ]; then
	# shellcheck source=encrypted-data.sh
	. "$(dirname "$0")/encrypted-data.sh"
	command -v cryptsetup >/dev/null || { echo "missing cryptsetup" >&2; exit 1; }
fi
if [ -b "$TARGET" ] && [ "${OROS_YES:-}" != "1" ]; then
	lsblk "$TARGET"
	read -r -p "Add a persistence partition to $TARGET? Type YES: " ok
	[ "$ok" = "YES" ] || exit 1
fi
[ -z "$ENCRYPT" ] || oros_passphrase

# Append one Linux partition that fills the rest of the device; with
# --encrypt first the orossys partition for system updates.
if [ -n "$ENCRYPT" ]; then
	echo "size=${OROS_SYS_MIB}MiB, type=L" | sfdisk --force --append --no-reread "$TARGET"
fi
echo ',,L' | sfdisk --force --append --no-reread "$TARGET"

PART="" SYS=""
# Loop devices made for an image file are let go at the end.
cleanup() {
	[ -f "$TARGET" ] || return 0
	for l in $PART $SYS; do losetup -d "$l" 2>/dev/null || true; done
}
trap cleanup EXIT
# part N: the device of the Nth partition from the end (1 = the last).
part() {
	if [ -f "$TARGET" ]; then
		# Image file: map just that partition with a loop device. Partition
		# nodes (loopNpM) need udev, which containers often lack.
		local start sectors
		read -r start sectors < <(partx -g -o START,SECTORS "$TARGET" | tail -n "$1" | head -n 1)
		losetup -f --show -o $((start * 512)) --sizelimit $((sectors * 512)) "$TARGET"
	else
		lsblk -lnpo NAME,TYPE "$TARGET" | awk '$2 == "part" { print $1 }' | tail -n "$1" | head -n 1
	fi
}
if [ -b "$TARGET" ]; then
	partprobe "$TARGET" || true
	udevadm settle || true
fi
PART="$(part 1)"
[ -n "$PART" ] || { echo "no new partition found" >&2; exit 1; }

if [ -n "$ENCRYPT" ]; then
	SYS="$(part 2)"
	[ -n "$SYS" ] || { echo "no orossys partition found" >&2; exit 1; }
	oros_make_data "$SYS" "$PART"
	echo "encrypted persistence ready on $PART (system updates on $SYS)"
	exit 0
fi

mkfs.ext4 -q -F -L persistence "$PART"
MNT="$(mktemp -d)"
mount "$PART" "$MNT"
echo "/home union" > "$MNT/persistence.conf"
umount "$MNT"
rmdir "$MNT"
echo "persistence partition ready on $PART"
