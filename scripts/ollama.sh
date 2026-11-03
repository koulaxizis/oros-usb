#!/bin/sh
# Pins the Ollama release that the local AI button downloads (oros-system
# ai, system/ai.go): the latest release of github.com/ollama/ollama, its
# Linux amd64 archive and that archive's SHA-256, checked against both the
# digest GitHub lists for the file and the release's own sha256sum.txt.
# Writes the JSON file $1; the image never carries Ollama itself.
# orOS Portable pins its Windows download with the asset name as $2.
#   scripts/ollama.sh OUT.json [ollama-windows-amd64.zip]
set -eu
OUT="$1"
API="https://api.github.com/repos/ollama/ollama/releases/latest"
NAME="${2:-ollama-linux-amd64.tar.zst}"
case "$NAME" in
	ollama-linux-amd64.tar.zst | ollama-windows-amd64.zip) ;;
	*) echo "unexpected Ollama asset $NAME" >&2; exit 1 ;;
esac
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
if [ -n "${GITHUB_TOKEN:-}" ]; then
	curl -fsSL --retry 3 -H "Authorization: Bearer $GITHUB_TOKEN" -o "$T/release.json" "$API"
else
	curl -fsSL --retry 3 -o "$T/release.json" "$API"
fi
jq -r --arg n "$NAME" '.tag_name as $t | [.assets[] | select(.name == $n)][0]
	| if . == null then error("no \($n) in the Ollama release \($t)") else "\($t) \(.browser_download_url) \((.digest // "") | sub("^sha256:"; ""))" end' \
	"$T/release.json" | tr -d '\r' > "$T/asset" # jq on Windows ends lines with CRLF
read -r TAG URL DIGEST < "$T/asset"
case "$URL" in
	https://github.com/ollama/ollama/releases/download/*/"$NAME") ;;
	*) echo "unexpected download address $URL" >&2; exit 1 ;;
esac
curl -fsSL --retry 3 -o "$T/sums" "https://github.com/ollama/ollama/releases/download/$TAG/sha256sum.txt"
SUM="$(awk -v n="$NAME" '$2 == n || $2 == "./" n { print $1 }' "$T/sums")"
echo "$SUM" | grep -Eq '^[0-9a-f]{64}$' || { echo "no checksum for $NAME in Ollama $TAG" >&2; exit 1; }
if [ -n "$DIGEST" ] && [ "$DIGEST" != "$SUM" ]; then
	echo "Ollama $TAG: GitHub's digest $DIGEST differs from sha256sum.txt $SUM" >&2
	exit 1
fi
jq -n --arg v "$TAG" --arg u "$URL" --arg s "$SUM" '{version: $v, url: $u, sha256: $s}' > "$OUT"
echo "Ollama $TAG -> $OUT"
