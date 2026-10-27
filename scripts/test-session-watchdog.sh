#!/bin/sh
# Runs oros-session's watchdog with a fake cage, without a VM: a window
# that never pings is restarted, the second time with the GPU off, and a
# window that pings is left alone.
#   scripts/test-session-watchdog.sh
set -eu
SESSION="$(cd "$(dirname "$0")/.." && pwd)/config/includes.chroot/usr/local/bin/oros-session"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin"
# The fake cage pings like the System extension, in mode "gpu" only once
# Chromium runs with the GPU off.
cat > "$T/bin/cage" <<'FAKE'
#!/bin/sh
if [ "$MODE" = gpu ]; then
	case "$OROS_PORTABLE_BROWSER_ARGS" in *--disable-gpu*) ;; *) exec sleep 1000 ;; esac
fi
while :; do date +%s%N > "$OROS_ALIVE"; sleep 2; done
FAKE
chmod +x "$T/bin/cage"

run() {
	rm -rf "$T/home"
	mkdir -p "$T/home"
	HOME="$T/home" XDG_RUNTIME_DIR="$T" PATH="$T/bin:$PATH" MODE="$1" \
		OROS_WATCHDOG_FIRST=10 OROS_WATCHDOG_QUIET=10 timeout "$2" sh "$SESSION" || true
	cat "$T/home/oros/data/session.log"
}

FAIL=0
LOG="$(run gpu 45)"
echo "$LOG"
for want in "restart 1 of 3 as before" "restart 2 of 3 with the GPU off" "start (GPU off)" "watchdog: orOS is showing"; do
	echo "$LOG" | grep -qF "$want" || { echo "FAIL: no \"$want\"" >&2; FAIL=1; }
done
echo "$LOG" | grep -q "restart 3" && { echo "FAIL: restarted after orOS showed" >&2; FAIL=1; }
LOG="$(run ok 25)"
echo "$LOG"
echo "$LOG" | grep -q "orOS is showing" || { echo "FAIL: ping not seen" >&2; FAIL=1; }
echo "$LOG" | grep -q "no sign of life" && { echo "FAIL: a pinging window was restarted" >&2; FAIL=1; }
[ "$FAIL" = 0 ] && echo "PASS: session watchdog"
exit "$FAIL"
