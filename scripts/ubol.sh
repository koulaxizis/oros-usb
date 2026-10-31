#!/bin/sh
# Unpacks uBlock Origin Lite (Raymond Hill, GPL-3.0) for Chromium into the
# folder $1, from the latest release of its official repository
# (github.com/uBlockOrigin/uBOL-home), checked against the SHA-256 GitHub
# lists for the file. The Internet window loads it (oros-system webArgs);
# its filter lists are inside the extension, so each monthly image brings
# fresh ones. Prints the version.
#   scripts/ubol.sh OUT_DIR
set -eu
OUT="$1"
API="https://api.github.com/repos/uBlockOrigin/uBOL-home/releases/latest"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
if [ -n "${GITHUB_TOKEN:-}" ]; then
	curl -fsSL --retry 3 -H "Authorization: Bearer $GITHUB_TOKEN" -o "$T/release.json" "$API"
else
	curl -fsSL --retry 3 -o "$T/release.json" "$API"
fi
jq -r '.tag_name as $t | [.assets[] | select((.name | ascii_downcase) as $n | ($n | endswith(".zip")) and ($n | contains("chromium")))][0]
	| if . == null then error("no Chromium build in the uBlock Origin Lite release \($t)") else "\($t) \(.browser_download_url) \((.digest // "") | sub("^sha256:"; ""))" end' \
	"$T/release.json" > "$T/asset"
read -r TAG URL SUM < "$T/asset"
case "$URL" in
	https://github.com/uBlockOrigin/uBOL-home/releases/download/*) ;;
	*) echo "unexpected download address $URL" >&2; exit 1 ;;
esac
curl -fsSL --retry 3 -o "$T/ubol.zip" "$URL"
if [ -n "$SUM" ]; then
	echo "$SUM  $T/ubol.zip" | sha256sum -c --quiet
fi
rm -rf "$OUT" && mkdir -p "$OUT"
unzip -q "$T/ubol.zip" -d "$T/x"
# The zip holds the extension either at its top or in one folder.
M="$(find "$T/x" -maxdepth 2 -name manifest.json | head -n1)"
[ -n "$M" ] || { echo "no manifest.json in uBlock Origin Lite $TAG" >&2; exit 1; }
cp -a "$(dirname "$M")/." "$OUT/"
jq -e '.manifest_version == 3' "$OUT/manifest.json" > /dev/null || { echo "uBlock Origin Lite $TAG is not a Manifest V3 extension" >&2; exit 1; }
echo "uBlock Origin Lite $TAG -> $OUT"
