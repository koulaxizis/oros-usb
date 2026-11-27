#!/usr/bin/env bash
# Checks the software center's catalog against the real sources, so a
# renamed app, a withdrawn add-on or a moved model fails CI and not a
# person's click (oros-system soft.go, addons.go, ai.go):
#   - every Flathub app exists on Flathub;
#   - every add-on is on the Chrome Web Store and is a Manifest V3
#     extension (Chromium no longer runs V2 ones);
#   - every model is in Ollama's library, about as large as the catalog says,
#     and so are the local AI's own models (aiModels in system/ai.go).
#   scripts/check-catalog.sh [catalog.json] [ai.go]
set -euo pipefail
CAT="${1:-catalog/catalog.json}"
AIGO="${2:-$(dirname "$0")/../system/ai.go}"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
bad=0
fail() { echo "FAIL $*"; bad=1; }

for id in $(jq -r '.items[] | select(.kind == "flatpak") | .id' "$CAT"); do
	code="$(curl -sS -o "$T/app.json" -w '%{http_code}' --retry 3 "https://flathub.org/api/v2/appstream/$id")"
	if [ "$code" = 200 ] && jq -e '.id' "$T/app.json" > /dev/null 2>&1; then
		echo "ok flathub $id: $(jq -r '.name' "$T/app.json")"
	else
		fail "flathub $id: HTTP $code"
	fi
done

for id in $(jq -r '.items[] | select(.kind == "addon") | .id' "$CAT"); do
	rm -rf "$T/crx" "$T/x.crx"
	if ! curl -fsSL --retry 3 -o "$T/x.crx" \
		"https://clients2.google.com/service/update2/crx?response=redirect&prodversion=142.0.0.0&acceptformat=crx2,crx3&x=id%3D$id%26uc"; then
		fail "add-on $id: not on the Chrome Web Store"
		continue
	fi
	# A CRX is a zip with a header in front; unzip skips it with a warning.
	mkdir -p "$T/crx"
	unzip -qo "$T/x.crx" manifest.json -d "$T/crx" 2>/dev/null || true
	if [ ! -s "$T/crx/manifest.json" ]; then
		fail "add-on $id: no manifest"
	elif jq -e '.manifest_version == 3' "$T/crx/manifest.json" > /dev/null; then
		echo "ok add-on $id: $(jq -r '.name' "$T/crx/manifest.json") $(jq -r '.version' "$T/crx/manifest.json") (MV3)"
	else
		fail "add-on $id: Manifest V$(jq -r '.manifest_version' "$T/crx/manifest.json")"
	fi
done

{
	jq -r '.models[] | "\(.name) \(.mb)"' "$CAT"
	sed -nE 's/^\t\{"([a-z0-9._-]+:[a-z0-9._-]+)", ([0-9]+),.*/\1 \2/p' "$AIGO"
} > "$T/models"
[ "$(grep -c '^qwen' "$T/models")" -ge 4 ] || fail "no local AI models read from $AIGO"
while read -r name mb; do
	repo="${name%%:*}"
	tag="${name#*:}"
	if ! curl -fsSL --retry 3 -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
		-o "$T/m.json" "https://registry.ollama.ai/v2/library/$repo/manifests/$tag"; then
		echo "FAIL model $name: not in Ollama's library"
		echo 1 > "$T/bad"
		continue
	fi
	real="$(jq '[.layers[].size] | add / 1000000 | floor' "$T/m.json")"
	if [ "$real" -lt $((mb * 8 / 10)) ] || [ "$real" -gt $((mb * 12 / 10)) ]; then
		echo "FAIL model $name: $real MB, the catalog says $mb"
		echo 1 > "$T/bad"
	else
		echo "ok model $name: $real MB"
	fi
done < "$T/models"
[ -f "$T/bad" ] && bad=1
exit "$bad"
