#!/usr/bin/env bash
# Boots the image the way a real stick would be used: written to a disk with
# a persistence partition, UEFI with Secure Boot on (Microsoft keys), in QEMU.
# Passes when orOS's browser profile with a service worker appears on the
# persistence partition and the System button reached oros-system.
# Screenshots go to the output folder. With DISK=disk.img it boots that
# disk instead (an install made by oros-system), and MARKER names a file
# the install copied, which must still be there.
#   sudo scripts/test-boot.sh out/orOS-USB-x.iso test-out
set -euo pipefail

ISO="$1"
OUT="${2:-test-out}"
WAIT="${BOOT_WAIT:-300}"
mkdir -p "$OUT"
if [ -n "${DISK:-}" ]; then
	IMG="$DISK"
else
	IMG="$OUT/usb.img"
	rm -f "$IMG"
	truncate -s 4G "$IMG"
	dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
	OROS_YES=1 "$(dirname "$0")/add-persistence.sh" "$IMG"
fi

cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd "$OUT/vars.fd"
MON="$OUT/monitor.sock"
qemu-system-x86_64 \
	-machine q35,smm=on -accel kvm -accel tcg -cpu max -m 4096 -smp 2 \
	-global driver=cfi.pflash01,property=secure,value=on \
	-drive if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd \
	-drive if=pflash,format=raw,unit=1,file="$OUT/vars.fd" \
	-drive file="$IMG",format=raw,if=virtio \
	-vga virtio -display none \
	-monitor unix:"$MON",server,nowait \
	-serial file:"$OUT/serial.log" &
QEMU=$!

mon() { echo "$1" | socat - UNIX-CONNECT:"$MON" >/dev/null 2>&1 || true; }
# The first seconds: firmware and boot menu.
for t in 4 8 12; do
	sleep 4
	mon "screendump $OUT/boot-$t.ppm"
done
sleep 18
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
# Small JPEG copies in the log ("SHOT name base64"), readable where the
# artifact is not.
for f in "$OUT"/boot-*.ppm "$OUT/screen-$WAIT.ppm" "$OUT/desktop.ppm" "$OUT/network.ppm"; do
	[ -f "$f" ] || continue
	echo "SHOT $(basename "${f%.ppm}") $(pnmscale -xsize 800 "$f" 2>/dev/null | pnmtojpeg -quality 70 2>/dev/null | base64 -w0)"
done
mon "system_powerdown"
for _ in $(seq 1 60); do kill -0 "$QEMU" 2>/dev/null || break; sleep 2; done
kill "$QEMU" 2>/dev/null || true
wait "$QEMU" 2>/dev/null || true
for f in "$OUT"/*.ppm; do [ -f "$f" ] && pnmtopng "$f" > "${f%.ppm}.png" && rm "$f"; done
# Screen text in the log, so a failure can be read without the artifact.
if command -v tesseract >/dev/null; then
	for f in $(ls "$OUT"/screen-*.png | sort -V); do
		echo "== $(basename "$f")"
		tesseract "$f" - 2>/dev/null | grep -v '^\s*$' | head -15 || true
	done
fi

read -r START SECTORS < <(partx -g -o START,SECTORS -n -1 "$IMG")
LOOP="$(losetup -f --show -o $((START * 512)) --sizelimit $((SECTORS * 512)) "$IMG")"
MNT="$(mktemp -d)"
mount -o ro "$LOOP" "$MNT"
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
# Saved Wi-Fi networks are kept next to the home folder.
if [ -z "$(find "$MNT" -maxdepth 4 -type d -name .oros-network | head -1)" ]; then
	echo "FAIL: no .oros-network folder for saved networks on the persistence partition" >&2
	OK=0
fi
SESSLOG="$(find "$MNT" -name session.log | head -1)"
[ -z "$SESSLOG" ] || { echo "== session.log"; tail -20 "$SESSLOG"; }
if [ -n "${MARKER:-}" ] && [ -z "$(find "$MNT" -name "$MARKER" | head -1)" ]; then
	echo "FAIL: $MARKER from the install is missing" >&2
	OK=0
fi
umount "$MNT"
losetup -d "$LOOP"
if [ "$OK" = 1 ]; then
	echo "PASS: orOS started, the System button works and its data is on the persistence partition"
else
	echo "FAIL: no orOS profile with a service worker on the persistence partition" >&2
	grep -aiE "pam|login|live-config|user-setup|oros|cage|chromium|error|fail" "$OUT/serial.log" | tail -120 >&2 || true
	exit 1
fi
