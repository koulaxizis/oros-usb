#!/bin/sh
# oros-app-link without a VM: which orOS ~/oros/app serves after a start,
# a downloaded update and a new image.
#   scripts/test-app-link.sh
set -eu
LINK="$(cd "$(dirname "$0")/.." && pwd)/config/includes.chroot/usr/local/bin/oros-app-link"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
FAIL=0
fail() { echo "FAIL: $*" >&2; FAIL=1; }

app() { # app DIR VERSION [BUILD]
	mkdir -p "$1"
	echo '<!doctype html>' > "$1/index.html"
	echo "  var APP_VERSION = \"$2\";   // bump" > "$1/shell.js"
	[ -z "${3:-}" ] || echo "$3" > "$1/.oros-build"
}
serves() { sed -n 's/.*APP_VERSION = "\([0-9.]*\)".*/\1/p' "$R/app/shell.js"; }

R="$T/home/oros"
mkdir -p "$R/data"
app "$T/image" 9.10.00 "beta.10 aaa"
sh "$LINK" "$R" "$T/image"
[ -L "$R/app" ] && [ "$(serves)" = 9.10.00 ] || fail "a first start links the image's orOS"

# The launcher swapped in a downloaded orOS (the link became app.old).
mv "$R/app" "$R/app.old"
app "$R/app" 9.16.00
sh "$LINK" "$R" "$T/image"
[ ! -L "$R/app" ] && [ "$(serves)" = 9.16.00 ] || fail "the same image keeps the downloaded orOS"

# A system update brings an image with an older orOS: the download stays.
rm -rf "$T/image"; app "$T/image" 9.12.00 "beta.11 bbb"
sh "$LINK" "$R" "$T/image"
[ ! -L "$R/app" ] && [ "$(serves)" = 9.16.00 ] || fail "a new image with an older orOS replaced the newer download"
[ ! -e "$R/app.old" ] || fail "app.old (the old link) is left behind"
grep -q "kept the downloaded orOS 9.16.00" "$R/data/session.log" || fail "no session.log line"

# A newer image (or the same version) wins and frees the copy.
rm -rf "$T/image"; app "$T/image" 9.16.00 "beta.12 ccc"
sh "$LINK" "$R" "$T/image"
[ -L "$R/app" ] && [ "$(serves)" = 9.16.00 ] || fail "an image with the same version must be used"
mv "$R/app" "$R/app.old"; app "$R/app" 9.16.01
rm -rf "$T/image"; app "$T/image" 10.0.0 "beta.13 ddd"
sh "$LINK" "$R" "$T/image"
[ -L "$R/app" ] && [ "$(serves)" = 10.0.0 ] || fail "a newer image's orOS must win (10.0.0 > 9.16.01)"

# A broken download (no index.html) falls back to the image.
rm -f "$R/app"; mkdir -p "$R/app"
sh "$LINK" "$R" "$T/image"
[ -L "$R/app" ] || fail "an app folder without index.html must give way to the image"

[ "$FAIL" = 0 ] && echo "oros-app-link: ok"
exit "$FAIL"
