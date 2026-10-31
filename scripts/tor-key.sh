#!/bin/sh
# Writes Tor Browser's signing key (Tor Browser Developers) to the keyring
# file $1, for oros-system's Tor button to check downloads with gpgv. The
# key is fetched from torproject.org (Web Key Directory), keys.openpgp.org
# as a fallback, and must match the fingerprint pinned here, the one
# torproject.org publishes.
#   scripts/tor-key.sh OUT.gpg
set -eu
OUT="$1"
FPR=EF6E286DDA85EA2A4BA7DE684E2C6E8793298290
G="$(mktemp -d)"
trap 'rm -rf "$G"' EXIT
for url in \
	"https://openpgpkey.torproject.org/.well-known/openpgpkey/torproject.org/hu/kounek7zrdx745qydx6p59t9mqjpuhdf" \
	"https://keys.openpgp.org/vks/v1/by-fingerprint/$FPR"; do
	if curl -fsSL --retry 3 -o "$G/key" "$url" && gpg --homedir "$G" --batch --quiet --import "$G/key" 2>/dev/null; then
		break
	fi
done
if ! gpg --homedir "$G" --batch --with-colons --fingerprint "$FPR" 2>/dev/null | grep -q "^fpr:*$FPR:"; then
	echo "Tor Browser's signing key $FPR not found" >&2
	exit 1
fi
gpg --homedir "$G" --batch --export "$FPR" > "$OUT"
test -s "$OUT"
echo "Tor Browser signing key $FPR -> $OUT"
