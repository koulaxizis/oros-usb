#!/usr/bin/env bash
# Writes "combo" sticks: the orOS USB image (boots orOS on any PC) plus an
# NTFS partition with orOS Portable (runs inside Windows or Linux without a
# restart). Writes several sticks at once, then verifies each one after
# re-reading it from the stick.
#
# Usage: sudo scripts/make-combo-sticks.sh [--yes] [--encrypt] [--persistence-gib N] ISO PORTABLE DEVICE...
#   ISO       orOS-USB-<version>.iso
#   PORTABLE  orOS-Portable-<version>.zip or an unpacked orOS-Portable folder
#   DEVICE    whole disks, e.g. /dev/sdb /dev/sdc (everything on them is erased)
#   --yes     skip the typed confirmation (for scripted runs)
#   --encrypt keep the data of the booted orOS encrypted (LUKS2) with one
#             passphrase for all the sticks, asked here twice (or taken from
#             OROS_PASSPHRASE) and on the orOS boot screen at every start
#   --persistence-gib  size of the partition that keeps data when orOS is
#             booted from the stick; default half of the space after the image
#
# Layout: the image's own partitions, then NTFS "OROS" with orOS Portable
# and SHA256SUMS.txt, then ext4 "persistence" ("/home union"). With
# --encrypt the last part is the orossys partition (5 GiB, system updates)
# and the LUKS2 data partition instead (scripts/encrypted-data.sh), inside
# an extended partition on sticks with an MBR (it holds only four).
# Needs: sfdisk, wipefs, partx, losetup, mkntfs (ntfs-3g), mkfs.ext4, unzip,
# sha256sum, cmp; cryptsetup for --encrypt.
set -euo pipefail

YES="" ENCRYPT="" PGIB="" ALLOW_ANY="${OROS_STICKS_ALLOW_ANY:-}"
while [ $# -gt 0 ]; do
	case "$1" in
		--yes) YES=1; shift ;;
		--encrypt) ENCRYPT=1; shift ;;
		--persistence-gib) PGIB="$2"; shift 2 ;;
		-h|--help) sed -n '2,26p' "$0"; exit 0 ;;
		*) break ;;
	esac
done
[ $# -ge 3 ] || { sed -n '2,26p' "$0"; exit 2; }
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 2; }
for cmd in sfdisk wipefs partx losetup mkntfs mkfs.ext4 unzip sha256sum cmp lsblk; do
	command -v "$cmd" >/dev/null || { echo "missing $cmd"; exit 2; }
done
[ -z "$PGIB" ] || [[ "$PGIB" =~ ^[1-9][0-9]*$ ]] || { echo "--persistence-gib needs a whole number"; exit 2; }
if [ -n "$ENCRYPT" ]; then
	command -v cryptsetup >/dev/null || { echo "missing cryptsetup"; exit 2; }
	# shellcheck source=encrypted-data.sh
	. "$(dirname "$0")/encrypted-data.sh"
fi

ISO="$1" SOURCE="$2"; shift 2
[ -f "$ISO" ] || { echo "no image $ISO"; exit 2; }
ISO_BYTES=$(stat -c %s "$ISO")
WORK="$(mktemp -d)"
trap 'umount "$WORK"/mnt-* 2>/dev/null || true; rm -rf "$WORK"' EXIT

# Unpack Portable once, find the folder that holds the launcher, list its checksums.
if [ -d "$SOURCE" ]; then
	FILES="$(cd "$SOURCE" && pwd)"
else
	unzip -q "$SOURCE" -d "$WORK/src"
	FILES="$(dirname "$(find "$WORK/src" -name orOS.exe -print -quit)")"
fi
[ -f "$FILES/orOS.exe" ] && [ -f "$FILES/orOS" ] || { echo "no orOS launcher in $SOURCE"; exit 2; }
(cd "$FILES" && find . -type f ! -name SHA256SUMS.txt -print0 | sort -z | xargs -0 sha256sum) > "$WORK/SHA256SUMS.txt"
NEED=$(( ISO_BYTES + $(du -sb "$FILES" | cut -f1) + 2 * 1024 * 1024 * 1024 ))
[ -z "$ENCRYPT" ] || NEED=$(( NEED + OROS_SYS_MIB * 1024 * 1024 ))
echo "image: $ISO ($((ISO_BYTES / 1024 / 1024)) MiB)"
echo "portable: $FILES ($(wc -l < "$WORK/SHA256SUMS.txt") files)"

# Safety checks: whole disks only, removable USB, nothing mounted from them,
# big enough, and not larger than any stick we would sell (256 GB).
for dev in "$@"; do
	[ -b "$dev" ] || { echo "$dev: not a block device"; exit 2; }
	type="$(lsblk -dno TYPE "$dev")"
	[ "$type" = disk ] || [ "$type" = loop ] || { echo "$dev: not a whole disk"; exit 2; }
	rm_flag="$(lsblk -dno RM "$dev" | tr -d ' ')"; tran="$(lsblk -dno TRAN "$dev" | tr -d ' ')"
	if [ -z "$ALLOW_ANY" ] && { [ "$rm_flag" != 1 ] || [ "$tran" != usb ]; }; then
		echo "$dev: not a removable USB disk (RM=$rm_flag TRAN=$tran); refusing"; exit 2
	fi
	if lsblk -nro MOUNTPOINT "$dev" | grep -qE '^/(boot.*)?$'; then
		echo "$dev: holds a system mount; refusing"; exit 2
	fi
	size=$(lsblk -bdno SIZE "$dev")
	[ "$size" -gt "$NEED" ] || { echo "$dev: too small"; exit 2; }
	[ "$size" -le $((256 * 1000 * 1000 * 1000)) ] || { echo "$dev: larger than 256 GB; refusing"; exit 2; }
done

echo
echo "These disks will be ERASED:"
for dev in "$@"; do
	printf '  %-14s %8s  %s\n' "$dev" "$(lsblk -dno SIZE "$dev" | tr -d ' ')" "$(lsblk -dno VENDOR,MODEL "$dev" | xargs)"
done
if [ -z "$YES" ]; then
	read -r -p "Type ERASE to continue: " answer
	[ "$answer" = ERASE ] || { echo "cancelled"; exit 1; }
fi
[ -z "$ENCRYPT" ] || oros_passphrase

# A loop device over partition N of a disk. Works the same on sticks and
# loop devices, without waiting for udev to make partition nodes.
part_loop() { # device number
	local start sectors
	read -r start sectors < <(partx -g -o START,SECTORS -n "$2" "$1")
	losetup -f --show -o $((start * 512)) --sizelimit $((sectors * 512)) "$1"
}

write_one() { # device
	local dev="$1" mnt="$WORK/mnt-$(basename "$1")" label free psize ntfs pers sys="" sysmib=0
	mkdir -p "$mnt"
	lsblk -nro NAME,MOUNTPOINT "$dev" | awk '$2!=""{print "/dev/"$1}' | xargs -r umount
	wipefs -qa "$dev"
	dd if="$ISO" of="$dev" bs=4M conv=fsync status=none
	blockdev --rereadpt "$dev" 2>/dev/null || true

	# Partitions 3 (NTFS, Portable) and 4 (persistence) after the image.
	label="$(sfdisk --dump "$dev" | awk '/^label:/{print $2}')"
	free=$(( ($(lsblk -bdno SIZE "$dev") - ISO_BYTES) / 1024 / 1024 ))
	[ -z "$ENCRYPT" ] || sysmib=$((OROS_SYS_MIB + 8))
	free=$((free - sysmib))
	if [ -n "$PGIB" ]; then psize=$((PGIB * 1024)); else psize=$((free / 2)); fi
	[ "$psize" -lt $((free - 1024)) ] || { echo "persistence does not fit"; return 1; }
	ntfs=7
	[ "$label" = gpt ] && ntfs=EBD0A0A2-B9E5-4433-87C0-68B6B72699C7
	printf 'size=%sMiB, type=%s\n' $((free - psize - 8)) "$ntfs" | sfdisk -q --force --append --no-reread "$dev"
	if [ -n "$ENCRYPT" ]; then
		# An MBR holds four partitions: the last two go into an extended one.
		[ "$label" = gpt ] || echo ',,Ex' | sfdisk -q --force --append --no-reread "$dev"
		echo "size=${OROS_SYS_MIB}MiB, type=L" | sfdisk -q --force --append --no-reread "$dev"
	fi
	echo ',,L' | sfdisk -q --force --append --no-reread "$dev"
	sfdisk -l "$dev"
	# The new partitions are the last ones (the image may have one or two;
	# an extended partition is listed before the ones inside it).
	local nrs
	if [ -n "$ENCRYPT" ]; then
		nrs=($(partx -g -o NR "$dev" | tail -4))
		[ "$label" = gpt ] && nrs=("${nrs[1]}" "${nrs[2]}" "${nrs[3]}") || nrs=("${nrs[0]}" "${nrs[2]}" "${nrs[3]}")
	else
		nrs=($(partx -g -o NR "$dev" | tail -2))
	fi

	ntfs="$(part_loop "$dev" "${nrs[0]}")"
	mkntfs -Q -q -L OROS "$ntfs" >/dev/null
	mount "$ntfs" "$mnt"
	cp -r "$FILES"/. "$mnt"/
	cp "$WORK/SHA256SUMS.txt" "$mnt/SHA256SUMS.txt"
	umount "$mnt"

	if [ -n "$ENCRYPT" ]; then
		sys="$(part_loop "$dev" "${nrs[1]}")"
		pers="$(part_loop "$dev" "${nrs[2]}")"
		oros_make_data "$sys" "$pers"
	else
		pers="$(part_loop "$dev" "${nrs[1]}")"
		mkfs.ext4 -q -F -L persistence "$pers"
		mount "$pers" "$mnt"
		echo "/home union" > "$mnt/persistence.conf"
		umount "$mnt"
	fi
	sync

	# Re-read from the stick itself, not the page cache.
	blockdev --flushbufs "$dev" "$ntfs" "$pers" $sys 2>/dev/null || true
	echo 3 > /proc/sys/vm/drop_caches 2>/dev/null || true
	# The image, byte for byte, except the MBR partition table we added to.
	cmp -n 446 "$ISO" "$dev"
	cmp -i 512 -n $((ISO_BYTES - 512)) "$ISO" "$dev"
	mount -o ro "$ntfs" "$mnt"
	(cd "$mnt" && sha256sum --quiet -c SHA256SUMS.txt)
	umount "$mnt"
	if [ -n "$ENCRYPT" ]; then
		local name="oros-check-$$-$(basename "$dev")"
		printf '%s' "$PASSPHRASE" | cryptsetup open --type luks2 --readonly --key-file=- "$pers" "$name"
		mount -o ro "/dev/mapper/$name" "$mnt"
		grep -qx "/home union" "$mnt/persistence.conf"
		umount "$mnt"
		cryptsetup close "$name"
		[ "$(blkid -s LABEL -o value "$sys")" = orossys ]
	else
		mount -o ro "$pers" "$mnt"
		grep -qx "/home union" "$mnt/persistence.conf"
		umount "$mnt"
	fi
	losetup -d "$ntfs" "$pers" $sys
	blockdev --rereadpt "$dev" 2>/dev/null || true
}

echo
declare -A PIDS
for dev in "$@"; do
	write_one "$dev" > "$WORK/$(basename "$dev").log" 2>&1 &
	PIDS[$dev]=$!
	echo "writing $dev"
done

FAIL=0
echo
for dev in "$@"; do
	if wait "${PIDS[$dev]}"; then
		echo "OK    $dev"
	else
		FAIL=1; echo "FAIL  $dev"
	fi
	sed 's/^/      /' "$WORK/$(basename "$dev").log"
done
exit $FAIL
