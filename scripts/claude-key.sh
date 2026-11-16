#!/bin/sh
# Writes the signing key of Anthropic's Claude desktop apt repository to
# the keyring file $1, for the Claude desktop button of oros-system to
# check the repository's InRelease with (system/claude.go). The key is
# fetched from downloads.claude.ai and must match the fingerprint pinned
# here, the one Anthropic's install guide publishes
# (https://code.claude.com/docs/en/desktop-linux).
#   scripts/claude-key.sh OUT.gpg
set -eu
OUT="$1"
FPR=31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE
G="$(mktemp -d)"
trap 'rm -rf "$G"' EXIT
curl -fsSL --retry 3 -o "$G/key" https://downloads.claude.ai/claude-desktop/key.asc
gpg --homedir "$G" --batch --quiet --import "$G/key" 2>/dev/null
if ! gpg --homedir "$G" --batch --with-colons --fingerprint "$FPR" 2>/dev/null | grep -q "^fpr:*$FPR:"; then
	echo "Claude desktop's signing key $FPR not found" >&2
	exit 1
fi
gpg --homedir "$G" --batch --export "$FPR" > "$OUT"
test -s "$OUT"
echo "Claude desktop signing key $FPR -> $OUT"
