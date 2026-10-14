#!/usr/bin/env bash
# Adds a "persistence" partition in the free space after the orOS USB image,
# so data and settings survive reboots. Works on a stick (/dev/sdX) or on a
# disk image file. Windows users can do the same with Rufus instead.
#   sudo scripts/add-persistence.sh /dev/sdX
set -euo pipefail

TARGET="${1:-}"
if [ -z "$TARGET" ] || { [ ! -b "$TARGET" ] && [ ! -f "$TARGET" ]; }; then
	echo "usage: $0 /dev/sdX | disk.img" >&2
	exit 1
fi
if [ -b "$TARGET" ] && [ "${OROS_YES:-}" != "1" ]; then
	lsblk "$TARGET"
	read -r -p "Add a persistence partition to $TARGET? Type YES: " ok
	[ "$ok" = "YES" ] || exit 1
fi

# Append one Linux partition that fills the rest of the device.
echo ',,L' | sfdisk --force --append --no-reread "$TARGET"

LOOP=""
DEV="$TARGET"
if [ -f "$TARGET" ]; then
	LOOP="$(losetup -fP --show "$TARGET")"
	DEV="$LOOP"
else
	partprobe "$TARGET" || true
	udevadm settle || true
fi
PART="$(lsblk -lnpo NAME,TYPE "$DEV" | awk '$2 == "part" { p = $1 } END { print p }')"
[ -n "$PART" ] || { echo "no new partition found" >&2; exit 1; }

mkfs.ext4 -q -F -L persistence "$PART"
MNT="$(mktemp -d)"
mount "$PART" "$MNT"
echo "/home union" > "$MNT/persistence.conf"
umount "$MNT"
rmdir "$MNT"
[ -z "$LOOP" ] || losetup -d "$LOOP"
echo "persistence partition ready on $PART"
