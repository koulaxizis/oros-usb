#!/usr/bin/env bash
# Checks the software center's catalog against the real sources, so a
# renamed app, a withdrawn add-on or a moved model fails CI and not a
# person's click (oros-system soft.go, addons.go, ai.go):
#   - every Flathub app exists on Flathub;
#   - every add-on is on the Chrome Web Store and is a Manifest V3
#     extension (Chromium no longer runs V2 ones);
#   - every model is in Ollama's library, about as large as the catalog says;
#   - GE-Proton's newest release still has the archive and checksum that
#     oros-system proton --update takes (proton.go).
#   scripts/check-catalog.sh [catalog.json]
set -euo pipefail
CAT="${1:-catalog/catalog.json}"
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

jq -r '.models[] | "\(.name) \(.mb)"' "$CAT" | while read -r name mb; do
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
done
auth=()
[ -n "${OROS_GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $OROS_GITHUB_TOKEN")
code="$(curl -sS -o "$T/ge.json" -w '%{http_code}' --retry 3 "${auth[@]}" -H 'Accept: application/vnd.github+json' \
	https://api.github.com/repos/GloriousEggroll/proton-ge-custom/releases/latest)"
tag="$(jq -r '.tag_name // empty' "$T/ge.json" 2>/dev/null || true)"
if [ "$code" = 200 ] && [[ "$tag" =~ ^GE-Proton[0-9]{1,3}-[0-9]{1,4}$ ]] \
	&& jq -e --arg t "$tag" '[.assets[].name] | (index($t + "-x86_64.tar.gz") and index($t + "-x86_64.sha512sum")) or (index($t + ".tar.gz") and index($t + ".sha512sum"))' "$T/ge.json" > /dev/null; then
	echo "ok GE-Proton $tag: $(jq -r '[.assets[].name] | join(", ")' "$T/ge.json")"
elif [ "$code" = 403 ] || [ "$code" = 429 ]; then
	echo "skip GE-Proton: GitHub's rate limit (HTTP $code)"
else
	fail "GE-Proton latest release: HTTP $code, tag '$tag', archive or checksum missing ($(jq -r '[.assets[]?.name] | join(", ")' "$T/ge.json" 2>/dev/null || true))"
fi

[ -f "$T/bad" ] && bad=1
exit "$bad"
