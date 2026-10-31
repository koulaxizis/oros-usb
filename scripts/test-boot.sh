#!/usr/bin/env bash
# Boots the image the way a real stick would be used: written to a disk with
# a persistence partition, UEFI with Secure Boot on (Microsoft keys), in QEMU.
# Passes when orOS's browser profile with a service worker appears on the
# persistence partition and the System button reached oros-system.
# Screenshots go to the output folder. With DISK=disk.img it boots that
# disk instead (an install made by oros-system), and MARKER names a file
# the install copied, which must still be there. With WATCHDOG_TEST=N the
# first N starts come up without the System extension (no ping), and the
# session watchdog must restart them, the second time with the GPU off.
# Without it, a start the watchdog had to restart fails the test. A USB
# stick labelled OROSTEST is plugged in too: the image's self-test opens it
# and writes to it through Files' computer files (oros-selftest-files).
# With OROS_TEST_PASSPHRASE the data is encrypted (add-persistence.sh
# --encrypt, or the DISK given): the test types a wrong passphrase on the
# boot screen first, then the right one, and checks both answers and that
# the prompt shows its text.
#   sudo scripts/test-boot.sh out/orOS-USB-x.iso test-out
set -euo pipefail

ISO="$1"
OUT="${2:-test-out}"
WAIT="${BOOT_WAIT:-300}"
mkdir -p "$OUT"
# shellcheck source=test-data.sh
. "$(dirname "$0")/test-data.sh"
PASS="${OROS_TEST_PASSPHRASE:-}"
[ -z "$PASS" ] || test_dm_prepare
if [ -n "${DISK:-}" ]; then
	IMG="$DISK"
else
	IMG="$OUT/usb.img"
	rm -f "$IMG"
	if [ -n "$PASS" ]; then
		truncate -s 10G "$IMG"
		dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
		OROS_YES=1 OROS_PASSPHRASE="$PASS" OROS_LUKS_TEST=1 "$(dirname "$0")/add-persistence.sh" --encrypt "$IMG"
	else
		truncate -s 4G "$IMG"
		dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
		OROS_YES=1 "$(dirname "$0")/add-persistence.sh" "$IMG"
	fi
fi

# Ask the image's self-test to save a network (oros-selftest.service).
data_mount "$IMG"
PMNT="$DATA_MNT"
if grep -qx "/ union" "$PMNT/persistence.conf" 2>/dev/null; then
	# The whole system is kept (Rufus): changes live in rw/.
	mkdir -p "$PMNT/rw/home"
	PHOME="$PMNT/rw/home"
else
	PHOME="$PMNT/home/rw"
fi
mkdir -p "$PHOME"
touch "$PHOME/.oros-selftest"
if [ -n "${WATCHDOG_TEST:-}" ]; then
	echo "$WATCHDOG_TEST" > "$PHOME/.oros-watchdog-test"
else
	rm -f "$PHOME/.oros-watchdog-test"
fi
data_umount

# A FAT32 USB stick for the computer files self-test (the Disks list
# leaves out anything under 1 GiB).
FSTICK="$OUT/usb-files.img"
rm -f "$FSTICK"
truncate -s 1100M "$FSTICK"
mkfs.vfat -F 32 -n OROSTEST "$FSTICK" >/dev/null

cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd "$OUT/vars.fd"
MON="$OUT/monitor.sock"
qemu-system-x86_64 \
	-machine q35,smm=on -accel kvm -accel tcg -cpu max -m 4096 -smp 2 \
	-global driver=cfi.pflash01,property=secure,value=on \
	-drive if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd \
	-drive if=pflash,format=raw,unit=1,file="$OUT/vars.fd" \
	-drive file="$IMG",format=raw,if=virtio \
	-device qemu-xhci -drive if=none,id=fstick,file="$FSTICK",format=raw \
	-device usb-storage,drive=fstick,removable=on \
	-vga virtio -display none \
	-monitor unix:"$MON",server,nowait \
	-serial file:"$OUT/serial.log" &
QEMU=$!

mon() { echo "$1" | socat - UNIX-CONNECT:"$MON" >/dev/null 2>&1 || true; }
# Encrypted data: oros-unlock writes "OROS-UNLOCK: asking" on the serial
# console each time it asks. The first answer is wrong, the second right.
UNLOCK_TYPED=0
type_text() {
	local s="$1" i c
	for ((i = 0; i < ${#s}; i++)); do
		c="${s:i:1}"
		case "$c" in -) c=minus ;; " ") c=spc ;; esac
		mon "sendkey $c"
		sleep 0.15
	done
	mon "sendkey ret"
}
unlock_step() {
	[ -n "$PASS" ] || return 0
	local asks
	asks="$(grep -ac 'OROS-UNLOCK: asking' "$OUT/serial.log" 2>/dev/null || true)"
	if [ "$UNLOCK_TYPED" = 0 ] && [ "${asks:-0}" -ge 1 ]; then
		sleep 2
		mon "screendump $OUT/unlock-ask.ppm"
		type_text "wrong-passphrase"
		UNLOCK_TYPED=1
	elif [ "$UNLOCK_TYPED" = 1 ] && [ "${asks:-0}" -ge 2 ]; then
		sleep 2
		mon "screendump $OUT/unlock-wrong.ppm"
		type_text "$PASS"
		UNLOCK_TYPED=2
	fi
}
# The first minute, every 3 seconds: firmware, the hidden boot menu, the
# orOS boot screen and the hand-over to orOS. None of it may show text.
for t in $(seq 3 3 60); do
	sleep 3
	mon "screendump $OUT/boot-$t.ppm"
	unlock_step
done
# A slow start can reach the passphrase after the first minute.
if [ -n "$PASS" ]; then
	for _ in $(seq 1 120); do
		[ "$UNLOCK_TYPED" = 2 ] && break
		unlock_step
		sleep 1
	done
fi
for t in $(seq 30 30 "$WAIT"); do
	sleep 30
	mon "screendump $OUT/screen-$t.ppm"
	kill -0 "$QEMU" 2>/dev/null || { echo "QEMU stopped early" >&2; exit 1; }
done
# Close the welcome tour, then look at the network screen (Ctrl+Alt+F2).
mon "sendkey esc"
sleep 5
mon "screendump $OUT/desktop.ppm"
mon "sendkey ctrl-alt-f2"
sleep 5
mon "screendump $OUT/network.ppm"
mon "sendkey ctrl-alt-f1"
sleep 3
# The orOS boot screen showed: a frame with the logo's gold (#c8a96e).
SPLASH=""
for f in $(ls "$OUT"/boot-*.ppm 2>/dev/null | sort -V); do
	if ppmhist -noheader "$f" 2>/dev/null | awk '$1 == 200 && $2 == 169 && $3 == 110 { found = 1 } END { exit !found }'; then
		SPLASH="$(basename "${f%.ppm}")"
		break
	fi
done
# Small JPEG copies in the log ("SHOT name base64"), readable where the
# artifact is not.
for f in "$OUT"/boot-*.ppm "$OUT"/unlock-*.ppm "$OUT/screen-$WAIT.ppm" "$OUT/desktop.ppm" "$OUT/network.ppm"; do
	[ -f "$f" ] || continue
	echo "SHOT $(basename "${f%.ppm}") $(pnmscale -xsize 800 "$f" 2>/dev/null | pnmtojpeg -quality 70 2>/dev/null | base64 -w0)"
done
mon "system_powerdown"
for _ in $(seq 1 60); do kill -0 "$QEMU" 2>/dev/null || break; sleep 2; done
kill "$QEMU" 2>/dev/null || true
wait "$QEMU" 2>/dev/null || true
for f in "$OUT"/*.ppm; do [ -f "$f" ] && pnmtopng "$f" > "${f%.ppm}.png" && rm "$f"; done
# Screen text in the log, so a failure can be read without the artifact.
# The boot frames must not show the text console: kernel, live-boot,
# systemd or a login (the firmware's own lines are not ours).
TEXT_SEEN=""
if command -v tesseract >/dev/null; then
	for f in $(ls "$OUT"/boot-*.png | sort -V); do
		words="$(tesseract "$f" - 2>/dev/null | grep -v '^\s*$' || true)"
		[ -n "$words" ] || continue
		echo "== $(basename "$f")"
		echo "$words" | head -8
		if echo "$words" | grep -qiE 'login:|GNU/Linux|Debian GNU|Begin:|Welcome to Debian|live-boot|tty1|\[ *OK *\]|Loading Linux|Loading initial ramdisk'; then
			TEXT_SEEN="$TEXT_SEEN $(basename "${f%.png}")"
		fi
	done
	for f in $(ls "$OUT"/screen-*.png | sort -V); do
		echo "== $(basename "$f")"
		tesseract "$f" - 2>/dev/null | grep -v '^\s*$' | head -15 || true
	done
fi
# The passphrase prompt: its text showed, a wrong answer was refused and
# asked again, the right one opened the data.
UNLOCK_FAIL=""
if [ -n "$PASS" ]; then
	echo "== serial console: oros-unlock"
	grep -a 'OROS-UNLOCK' "$OUT/serial.log" || true
	grep -aq 'OROS-UNLOCK: wrong' "$OUT/serial.log" || UNLOCK_FAIL="$UNLOCK_FAIL; a wrong passphrase was not refused"
	grep -aq 'OROS-UNLOCK: unlocked' "$OUT/serial.log" || UNLOCK_FAIL="$UNLOCK_FAIL; the right passphrase did not open the data"
	if command -v tesseract >/dev/null && [ -f "$OUT/unlock-ask.png" ]; then
		words="$(tesseract "$OUT/unlock-ask.png" - 2>/dev/null | grep -v '^\s*$' || true)"
		echo "== unlock-ask"; echo "$words" | head -5
		echo "$words" | grep -qi 'passphrase' || UNLOCK_FAIL="$UNLOCK_FAIL; the prompt showed no text"
	fi
fi

data_mount "$IMG" ro
MNT="$DATA_MNT"
PROFILE="$(find "$MNT" -type d -path '*oros/data/profile/Default' | head -1)"
ls -la "$MNT" "$PROFILE" 2>&1 | tee "$OUT/persistence.txt" || true
OK=0
[ -n "$PROFILE" ] && [ -d "$PROFILE/Service Worker" ] && OK=1
SYSLOG="$(find "$MNT" -name oros-system.log | head -1)"
if [ -n "$SYSLOG" ]; then
	echo "== oros-system.log"
	tail -5 "$SYSLOG"
	grep -q "cmd status" "$SYSLOG" || OK=0
else
	echo "FAIL: the System button never reached oros-system" >&2
	OK=0
fi
# Saved networks are kept next to the home folder: the self-test's network
# must be there.
if [ -z "$(find "$MNT" -maxdepth 5 -path '*/.oros-network/oros-selftest.nmconnection' | head -1)" ]; then
	echo "FAIL: NetworkManager could not save a network in /home/.oros-network" >&2
	ls -la "$(find "$MNT" -maxdepth 4 -type d -name .oros-network | head -1)" 2>&1 || true
	grep -a "oros-selftest\|NetworkManager" "$OUT/serial.log" | tail -20 >&2 || true
	OK=0
fi
# The time zone set through the System button's sudo rule is kept too.
TZF="$(find "$MNT" -maxdepth 4 -name .oros-timezone | head -1)"
if [ -z "$TZF" ] || [ "$(cat "$TZF")" != "Europe/Athens" ]; then
	echo "FAIL: the time zone was not saved in /home/.oros-timezone" >&2
	grep -a "oros-selftest\|sudo" "$OUT/serial.log" | tail -20 >&2 || true
	OK=0
fi
# The print server runs and Bluetooth pairings are kept on the stick.
DEVF="$(find "$MNT" -maxdepth 5 -name .oros-selftest-devices | head -1)"
[ -z "$DEVF" ] || { echo "== devices self-test"; cat "$DEVF"; }
if [ -z "$DEVF" ] || ! grep -q "scheduler is running" "$DEVF"; then
	echo "FAIL: the print server (CUPS) is not running" >&2
	OK=0
fi
if [ -z "$DEVF" ] || ! grep -qx "/var/lib/bluetooth" "$DEVF"; then
	echo "FAIL: Bluetooth pairings are not kept in /home/.oros-bluetooth" >&2
	OK=0
fi
# Files' computer files: the launcher's API opened the stick writable,
# wrote to it and to the home folder, and closed it again.
FILESF="$(find "$MNT" -maxdepth 5 -name .oros-selftest-files | head -1)"
[ -z "$FILESF" ] || { echo "== computer files self-test"; cat "$FILESF"; }
if [ -n "${WATCHDOG_TEST:-}" ]; then
	:
elif [ -z "$FILESF" ] || ! grep -qx "PASS" "$FILESF"; then
	echo "FAIL: Files' computer files could not open and write the USB stick" >&2
	grep -a "selftest-files\|oros-portable\|computer files" "$OUT/serial.log" | tail -20 >&2 || true
	OK=0
elif [ "$(mtype -i "$FSTICK" ::hello.txt 2>/dev/null)" != "orOS computer files" ]; then
	echo "FAIL: hello.txt written through Files is not on the USB stick" >&2
	OK=0
fi
# orOS Bridge starts with the session (oros-kiosk) and listens for Studio.
BRIDGELOG="$(find "$MNT" -name bridge.log | head -1)"
[ -z "$BRIDGELOG" ] || { echo "== bridge.log"; tail -5 "$BRIDGELOG"; }
if [ -n "${WATCHDOG_TEST:-}" ]; then
	:
elif [ -z "$BRIDGELOG" ] || ! grep -q "is ready on 127.0.0.1:47621" "$BRIDGELOG"; then
	echo "FAIL: orOS Bridge did not start in the session" >&2
	OK=0
fi
# The system log is kept for the problem report.
if [ -z "$(find "$MNT" -path '*/.oros-journal/*' -name '*.journal' | head -1)" ]; then
	echo "FAIL: no system log in /home/.oros-journal on the persistence partition" >&2
	OK=0
fi
SESSLOG="$(find "$MNT" -name session.log | head -1)"
[ -z "$SESSLOG" ] || { echo "== session.log"; tail -20 "$SESSLOG"; }
if [ -n "${WATCHDOG_TEST:-}" ]; then
	# The sabotaged starts were restarted, the second with the GPU off, and
	# orOS then showed with the GPU off.
	for want in "restart 1 of 3 as before" "restart 2 of 3 with the GPU off" "start (GPU off)"; do
		grep -qF "$want" "$SESSLOG" 2>/dev/null || { echo "FAIL: watchdog: no \"$want\" in session.log" >&2; OK=0; }
	done
	sed -n '/start (GPU off)/,$p' "$SESSLOG" 2>/dev/null | grep -q "watchdog: orOS is showing" ||
		{ echo "FAIL: watchdog: orOS did not show after the restart with the GPU off" >&2; OK=0; }
elif grep -q "watchdog: no sign of life" "$SESSLOG" 2>/dev/null; then
	echo "FAIL: the session watchdog had to restart orOS on a normal start" >&2
	OK=0
fi
# A graphical start from the menu to orOS.
if [ -n "$TEXT_SEEN" ]; then
	echo "FAIL: text console on the screen while starting:$TEXT_SEEN" >&2
	OK=0
fi
if [ -n "$SPLASH" ]; then
	echo "== the orOS boot screen showed ($SPLASH)"
else
	echo "FAIL: the orOS boot screen (Plymouth) never showed in the first minute" >&2
	OK=0
fi
if [ -n "${MARKER:-}" ] && [ -z "$(find "$MNT" -name "$MARKER" | head -1)" ]; then
	echo "FAIL: $MARKER from the install is missing" >&2
	OK=0
fi
if [ -n "$UNLOCK_FAIL" ]; then
	echo "FAIL: encrypted data${UNLOCK_FAIL}" >&2
	OK=0
fi
data_umount
if [ "$OK" = 1 ]; then
	echo "PASS: orOS started, the System button works and its data is on the persistence partition"
else
	echo "FAIL: no orOS profile with a service worker on the persistence partition" >&2
	grep -aiE "pam|login|live-config|user-setup|oros|cage|chromium|error|fail" "$OUT/serial.log" | tail -120 >&2 || true
	exit 1
fi
