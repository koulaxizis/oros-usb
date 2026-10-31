# Shared by add-persistence.sh and make-combo-sticks.sh (sourced, not run):
# the optional encrypted data of an orOS stick. Same layout and settings as
# "oros-system install --encrypt" (system/crypt.go):
#   ext4 "orossys" (5 GiB)  system updates (slots A/B and boot.cfg); GRUB
#                           reads them before the passphrase is asked, so
#                           they cannot be encrypted (they hold only the
#                           public orOS system)
#   LUKS2 "oros-persistence" with ext4 "persistence" inside ("/home union")
# The boot asks for the passphrase on the orOS boot screen
# (config/includes.chroot/etc/initramfs-tools/scripts/live-realpremount/oros-unlock).

OROS_SYS_MIB=5120
OROS_MIN_PASSPHRASE=10

# oros_passphrase: OROS_PASSPHRASE from the environment (scripted runs and
# tests), or asked twice on the terminal. Sets PASSPHRASE.
oros_passphrase() {
	if [ -n "${OROS_PASSPHRASE:-}" ]; then
		PASSPHRASE="$OROS_PASSPHRASE"
	else
		local again
		read -r -s -p "Passphrase for the encrypted data (at least $OROS_MIN_PASSPHRASE characters): " PASSPHRASE; echo
		read -r -s -p "The same passphrase again: " again; echo
		[ "$PASSPHRASE" = "$again" ] || { echo "the two passphrases differ" >&2; return 1; }
	fi
	[ "${#PASSPHRASE}" -ge "$OROS_MIN_PASSPHRASE" ] ||
		{ echo "the passphrase needs at least $OROS_MIN_PASSPHRASE characters" >&2; return 1; }
	# The boot screen types with the US English layout: other letters could
	# never be typed there.
	if printf '%s' "$PASSPHRASE" | LC_ALL=C grep -q '[^ -~]'; then
		echo "use only Latin letters, digits, spaces and symbols of a US keyboard" >&2; return 1
	fi
	echo "Keep the passphrase safe: without it the data on the stick cannot be read, by anyone."
}

# oros_luks_format DEVICE LABEL: LUKS2 with Argon2id (256 MiB, about 2 s
# on this computer). OROS_LUKS_TEST=1 (CI only) keeps Argon2id but makes it
# fast, because the test VM may run without hardware acceleration.
oros_luks_format() {
	local kdf=(--pbkdf argon2id --pbkdf-memory 262144 --pbkdf-parallel 4 --iter-time 2000)
	[ "${OROS_LUKS_TEST:-}" != 1 ] || kdf=(--pbkdf argon2id --pbkdf-memory 32768 --pbkdf-parallel 1 --pbkdf-force-iterations 4)
	printf '%s' "$PASSPHRASE" | cryptsetup luksFormat --batch-mode --type luks2 --label "$2" \
		"${kdf[@]}" --key-file=- "$1"
}

# oros_make_data SYSDEV DATADEV: formats the orossys partition and the
# encrypted data partition, with persistence.conf inside.
oros_make_data() {
	local sys="$1" data="$2" name="oros-new-$$-$RANDOM" mnt
	mkfs.ext4 -q -F -L orossys "$sys"
	oros_luks_format "$data" oros-persistence
	mkdir -p /run/cryptsetup
	printf '%s' "$PASSPHRASE" | cryptsetup open --type luks2 --key-file=- "$data" "$name"
	mkfs.ext4 -q -F -L persistence "/dev/mapper/$name"
	mnt="$(mktemp -d)"
	mount "/dev/mapper/$name" "$mnt"
	echo "/home union" > "$mnt/persistence.conf"
	umount "$mnt"
	rmdir "$mnt"
	cryptsetup close "$name"
}
