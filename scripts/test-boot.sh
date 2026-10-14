#!/usr/bin/env bash
# Boots the image the way a real stick would be used: written to a disk with
# a persistence partition, UEFI with Secure Boot on (Microsoft keys), in QEMU.
# Passes when orOS's browser profile with a service worker appears on the
# persistence partition. Screenshots go to the output folder.
#   sudo scripts/test-boot.sh out/orOS-USB-x.iso test-out
set -euo pipefail

ISO="$1"
OUT="${2:-test-out}"
WAIT="${BOOT_WAIT:-300}"
mkdir -p "$OUT"
IMG="$OUT/usb.img"
rm -f "$IMG"
truncate -s 4G "$IMG"
dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
OROS_YES=1 "$(dirname "$0")/add-persistence.sh" "$IMG"

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
for t in $(seq 30 30 "$WAIT"); do
	sleep 30
	mon "screendump $OUT/screen-$t.ppm"
	kill -0 "$QEMU" 2>/dev/null || { echo "QEMU stopped early" >&2; exit 1; }
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
umount "$MNT"
losetup -d "$LOOP"
if [ "$OK" = 1 ]; then
	echo "PASS: orOS started and its data is on the persistence partition"
else
	echo "FAIL: no orOS profile with a service worker on the persistence partition" >&2
	tail -50 "$OUT/serial.log" >&2 || true
	exit 1
fi
