# Shared by the boot tests (sourced, not run): opens and mounts the data
# partition of a stick or disk image, the last partition, whether it is
# plain ext4 or encrypted (LUKS2, then OROS_TEST_PASSPHRASE opens it).
#   data_mount IMG [mount options]   sets DATA_MNT
#   data_umount
#   part_mount IMG N [mount options] the Nth partition from the end; sets PART_MNT
#   part_umount

# cryptsetup in a CI container: no udev, so device-mapper makes its own
# nodes; the control node loads the kernel module when it is opened.
test_dm_prepare() {
	export DM_DISABLE_UDEV=1
	mkdir -p /dev/mapper /run/cryptsetup
	[ -e /dev/mapper/control ] || mknod /dev/mapper/control c 10 236
}

DATA_MNT="" DATA_LOOP="" DATA_MAP=""
data_mount() { # image [mount options]
	local start sectors dev
	read -r start sectors < <(partx -g -o START,SECTORS -n -1 "$1")
	DATA_LOOP="$(losetup -f --show -o $((start * 512)) --sizelimit $((sectors * 512)) "$1")"
	dev="$DATA_LOOP" DATA_MAP=""
	if cryptsetup isLuks "$DATA_LOOP" 2>/dev/null; then
		[ -n "${OROS_TEST_PASSPHRASE:-}" ] || { echo "encrypted data but no OROS_TEST_PASSPHRASE" >&2; return 1; }
		test_dm_prepare
		DATA_MAP="oros-test-$$-$RANDOM"
		printf '%s' "$OROS_TEST_PASSPHRASE" | cryptsetup open --type luks2 --key-file=- "$DATA_LOOP" "$DATA_MAP"
		dev="/dev/mapper/$DATA_MAP"
	fi
	DATA_MNT="$(mktemp -d)"
	mount ${2:+-o "$2"} "$dev" "$DATA_MNT"
}
data_umount() {
	umount "$DATA_MNT"
	rmdir "$DATA_MNT"
	[ -z "$DATA_MAP" ] || cryptsetup close "$DATA_MAP"
	losetup -d "$DATA_LOOP"
}

PART_MNT="" PART_LOOP=""
part_mount() { # image N [mount options]
	local start sectors
	read -r start sectors < <(partx -g -o START,SECTORS "$1" | tail -n "$2" | head -n 1)
	PART_LOOP="$(losetup -f --show -o $((start * 512)) --sizelimit $((sectors * 512)) "$1")"
	PART_MNT="$(mktemp -d)"
	mount ${3:+-o "$3"} "$PART_LOOP" "$PART_MNT"
}
part_umount() {
	umount "$PART_MNT"
	rmdir "$PART_MNT"
	losetup -d "$PART_LOOP"
}
