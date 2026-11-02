#!/bin/sh
# Copies the orOS add-on (koulaxizis/oros extension/, MIT) from $1 into the
# folder $2 for the orOS window of this stick and of orOS Portable: its
# orOS address is preset to the launcher's (http://localhost:47800/) and
# it may read that page without asking, so Passwords fill and save work
# in the Internet window, which opens in the same browser profile as orOS
# (oros-system webArgs, oros-portable launcher/sysweb.go). Nothing else in
# the add-on changes. Prints the version.
#   scripts/addon.sh OROS_EXTENSION_DIR OUT_DIR
set -eu
SRC="$1"
OUT="$2"
BASE="http://localhost:47800/"
[ -f "$SRC/manifest.json" ] && [ -f "$SRC/core.js" ] || { echo "no orOS add-on in $SRC" >&2; exit 1; }
rm -rf "$OUT"
cp -R "$SRC" "$OUT"
# Exactly one default address, or the add-on's code changed under us.
[ "$(grep -c '^  var DEFAULT_BASE = "https://useoros.online/";$' "$OUT/core.js")" = 1 ] ||
	{ echo "core.js: DEFAULT_BASE not found" >&2; exit 1; }
sed -i "s|^  var DEFAULT_BASE = \"https://useoros.online/\";\$|  var DEFAULT_BASE = \"$BASE\";|" "$OUT/core.js"
grep -q "DEFAULT_BASE = \"$BASE\"" "$OUT/core.js"
jq '.host_permissions = ((.host_permissions // []) + ["http://localhost/*", "http://127.0.0.1/*"] | unique)' \
	"$OUT/manifest.json" > "$OUT/manifest.json.new"
mv "$OUT/manifest.json.new" "$OUT/manifest.json"
jq -r '"orOS add-on " + .version' "$OUT/manifest.json"
