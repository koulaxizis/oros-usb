#!/usr/bin/env bash
# Measures orOS multitasking on the stick itself: the image boots in QEMU
# (4 GB, 2 CPUs, like a weak PC) with a test copy of orOS on the
# persistence partition whose index.html also loads scripts/qa-mt.js.
# That script opens apps with multitasking off, on, and with heavy apps,
# and prints each step on the console (session.log). Meanwhile this
# script reads the guest's memory (virtio-balloon statistics) and the CPU
# QEMU used, every 5 s. Report only; it does not fail on numbers.
#   sudo scripts/test-multitask.sh orOS-USB-x.iso <orOS checkout> test-out
set -euo pipefail

ISO="$1"
OROS="$2"
OUT="${3:-test-out}"
WAIT="${MT_WAIT:-1500}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
IMG="$OUT/usb.img"
rm -f "$IMG"
truncate -s 6G "$IMG"
dd if="$ISO" of="$IMG" conv=notrunc bs=4M status=none
OROS_YES=1 "$(dirname "$0")/add-persistence.sh" "$IMG"

# The image's orOS build: oros-session keeps ~/oros/app only while it
# matches, so the test copy must carry the same mark.
SQ="$(mktemp -d)"
xorriso -osirrox on -indev "$ISO" -extract /live/filesystem.squashfs "$SQ/fs" >/dev/null 2>&1
BUILD="$(unsquashfs -cat "$SQ/fs" opt/oros/app/.oros-build)"
OUID="$(unsquashfs -cat "$SQ/fs" etc/passwd | awk -F: '$1 == "oros" { print $3 ":" $4 }')"
rm -rf "$SQ"
echo "image orOS build: $BUILD, oros user $OUID"

read -r PSTART PSECTORS < <(partx -g -o START,SECTORS -n -1 "$IMG")
PLOOP="$(losetup -f --show -o $((PSTART * 512)) --sizelimit $((PSECTORS * 512)) "$IMG")"
PMNT="$(mktemp -d)"
mount "$PLOOP" "$PMNT"
HOMEDIR="$PMNT/home/rw"
grep -qx "/ union" "$PMNT/persistence.conf" 2>/dev/null && HOMEDIR="$PMNT/rw/home"
APP="$HOMEDIR/oros/oros/app"
mkdir -p "$APP"
tar -C "$OROS" --exclude=.git --exclude=.github --exclude=tests --exclude=android --exclude=node_modules -cf - . | tar -C "$APP" -xf -
cp "$(dirname "$0")/qa-mt.js" "$APP/qa-mt.js"
sed -i 's|</body>|<script src="qa-mt.js"></script>\n</body>|' "$APP/index.html"
grep -q 'qa-mt.js' "$APP/index.html"
echo "$BUILD" > "$APP/.oros-build"
echo "$BUILD" > "$HOMEDIR/oros/oros/.image-build"
chown -R "$OUID" "$HOMEDIR/oros"
umount "$PMNT"
losetup -d "$PLOOP"

cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd "$OUT/vars.fd"
MON="$OUT/monitor.sock"
QMP="$OUT/qmp.sock"
qemu-system-x86_64 \
	-machine q35,smm=on -accel kvm -accel tcg -cpu max -m 4096 -smp 2 \
	-global driver=cfi.pflash01,property=secure,value=on \
	-drive if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd \
	-drive if=pflash,format=raw,unit=1,file="$OUT/vars.fd" \
	-drive file="$IMG",format=raw,if=virtio \
	-device virtio-balloon-pci,id=bal \
	-vga virtio -display none \
	-monitor unix:"$MON",server,nowait \
	-qmp unix:"$QMP",server,nowait \
	-serial file:"$OUT/serial.log" &
QEMU=$!

mon() { echo "$1" | socat - UNIX-CONNECT:"$MON" >/dev/null 2>&1 || true; }
qmp() {
	printf '{"execute":"qmp_capabilities"}\n%s\n' "$1" | socat -t 2 - UNIX-CONNECT:"$QMP" 2>/dev/null | tail -1 || true
}
sleep 5
qmp '{"execute":"qom-set","arguments":{"path":"/machine/peripheral/bal","property":"guest-stats-polling-interval","value":5}}' >/dev/null
HZ="$(getconf CLK_TCK)"
: > "$OUT/samples.tsv"
START=$(date +%s)
N=0
while [ $(( $(date +%s) - START )) -lt "$WAIT" ]; do
	kill -0 "$QEMU" 2>/dev/null || { echo "QEMU stopped early" >&2; exit 1; }
	S="$(qmp '{"execute":"qom-get","arguments":{"path":"/machine/peripheral/bal","property":"guest-stats"}}')"
	AV="$(echo "$S" | grep -o '"stat-available-memory": *[0-9]*' | grep -o '[0-9]*$' || true)"
	FR="$(echo "$S" | grep -o '"stat-free-memory": *[0-9]*' | grep -o '[0-9]*$' || true)"
	CPU="$(awk -v hz="$HZ" '{ print ($14 + $15) / hz }' "/proc/$QEMU/stat")"
	printf '%s\t%s\t%s\t%s\n' "$(date +%s%3N)" "$(( ${AV:-0} / 1048576 ))" "$(( ${FR:-0} / 1048576 ))" "$CPU" >> "$OUT/samples.tsv"
	N=$((N + 1))
	[ $((N % 60)) -eq 0 ] && mon "screendump $OUT/screen-$N.ppm"
	sleep 5
done
mon "screendump $OUT/end.ppm"
mon "system_powerdown"
for _ in $(seq 1 60); do kill -0 "$QEMU" 2>/dev/null || break; sleep 2; done
kill "$QEMU" 2>/dev/null || true
wait "$QEMU" 2>/dev/null || true
for f in "$OUT"/*.ppm; do [ -f "$f" ] && pnmtopng "$f" > "${f%.ppm}.png" && rm "$f"; done

LOOP="$(losetup -f --show -o $((PSTART * 512)) --sizelimit $((PSECTORS * 512)) "$IMG")"
MNT="$(mktemp -d)"
mount -o ro "$LOOP" "$MNT"
SESSLOG="$(find "$MNT" -name session.log | head -1)"
SAVED="$(find "$MNT" -name 'oros-mt*.jsonl' | head -1)"
if [ -n "$SAVED" ]; then cp "$SAVED" "$OUT/steps.jsonl"
else grep -ao 'OROS-MT {.*}' "$SESSLOG" | sed 's/^OROS-MT //' > "$OUT/steps.jsonl" || true; fi
[ -s "$OUT/steps.jsonl" ] || { echo "no OROS-MT lines; session.log:"; tail -40 "$SESSLOG"; }
umount "$MNT"
losetup -d "$LOOP"
rm -f "$IMG"

# Each step with the guest's available memory and QEMU's CPU at that time.
python3 - "$OUT/steps.jsonl" "$OUT/samples.tsv" <<'PY' | tee "$OUT/report.md"
import json, sys
steps = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
samples = [tuple(float(x) for x in l.split("\t")) for l in open(sys.argv[2]) if l.strip()]
def at(t):
    best = None
    for s in samples:
        if s[0] <= t + 6000: best = s
    return best
print("| pass | step | app | ms | reloaded | JS heap MB | frames | guest available MB |")
print("|---|---|---|---|---|---|---|---|")
idle = {}
for s in steps:
    smp = at(s["t"])
    print("| %s | %s | %s | %s | %s | %s | %s | %s |" % (s.get("pass", ""), s["ev"], s.get("app", ""), s.get("ms", ""),
        s.get("reloaded", ""), s.get("heapMB", ""), s.get("frames", ""), int(smp[1]) if smp else ""))
    if s["ev"] in ("idle-start", "idle-end"): idle[(s["pass"], s["ev"])] = smp
print()
for p in ("off", "on", "heavy"):
    a, b = idle.get((p, "idle-start")), idle.get((p, "idle-end"))
    if a and b and b[0] > a[0]:
        print("%s: idle CPU %.1f%% of one core (QEMU, 2 vCPUs)" % (p, (b[3] - a[3]) / ((b[0] - a[0]) / 1000) * 100))
PY
