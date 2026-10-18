#!/usr/bin/env bash
# Boots a stick made the way Rufus makes one on Windows ("ISO image mode"
# with a persistent partition): the ISO's files copied to a FAT32
# partition, then an ext3 partition labelled "persistence" whose
# persistence.conf keeps the whole system ("/ union"), not only /home.
# Then checks it like test-boot.sh, on this first start.
#   sudo scripts/test-rufus.sh out/orOS-USB-x.iso test-out/rufus
set -euo pipefail

ISO="$1"
OUT="${2:-test-out/rufus}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$OUT"
IMG="$OUT/rufus.img"
rm -f "$IMG"
truncate -s 8G "$IMG"
FATMIB=$(( $(stat -c %s "$ISO") / 1024 / 1024 + 512 ))
printf 'size=%sMiB, type=c, bootable\nsize=4GiB, type=83\n' "$FATMIB" | sfdisk -q "$IMG"

part_loop() { # number
	local start sectors
	read -r start sectors < <(partx -g -o START,SECTORS -n "$1" "$IMG")
	losetup -f --show -o $((start * 512)) --sizelimit $((sectors * 512)) "$IMG"
}
ISOMNT="$(mktemp -d)" MNT="$(mktemp -d)"
mount -o loop,ro "$ISO" "$ISOMNT"
FAT="$(part_loop 1)"
mkfs.vfat -F 32 -n OROS "$FAT" >/dev/null
mount "$FAT" "$MNT"
# FAT has no links: copy folders and files only (the ISO has "debian -> .").
(cd "$ISOMNT" && find . -type d -print0 | xargs -0 -I{} mkdir -p "$MNT/{}")
(cd "$ISOMNT" && find . -type f -print0 | xargs -0 -I{} cp {} "$MNT/{}")
umount "$MNT"
losetup -d "$FAT"
umount "$ISOMNT"

PERS="$(part_loop 2)"
mkfs.ext3 -q -F -L persistence "$PERS"
mount "$PERS" "$MNT"
echo "/ union" > "$MNT/persistence.conf"
umount "$MNT"
losetup -d "$PERS"
sfdisk -l "$IMG"

DISK="$IMG" "$ROOT/scripts/test-boot.sh" "$ISO" "$OUT"
rm -f "$IMG"
