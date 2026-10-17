#!/usr/bin/env bash
# Takes the system out of an orOS USB image for "oros-system update":
# filesystem.squashfs, vmlinuz, initrd.img and update.json (version, sizes,
# SHA-256) in the output folder. Releases carry these files.
#   scripts/update-assets.sh out/orOS-USB-x.iso out/update x
set -euo pipefail

ISO="$1" OUT="$2" VERSION="$3"
mkdir -p "$OUT"
list="$(xorriso -indev "$ISO" -ls /live 2>/dev/null | tr -d "'")"
kernel="$(echo "$list" | grep -m1 -E '^vmlinuz')"
initrd="$(echo "$list" | grep -m1 -E '^initrd\.img')"
[ -n "$kernel" ] && [ -n "$initrd" ] || { echo "no kernel or initrd in $ISO" >&2; exit 1; }
xorriso -osirrox on -indev "$ISO" \
	-extract /live/filesystem.squashfs "$OUT/filesystem.squashfs" \
	-extract "/live/$kernel" "$OUT/vmlinuz" \
	-extract "/live/$initrd" "$OUT/initrd.img" >/dev/null 2>&1
chmod 644 "$OUT"/filesystem.squashfs "$OUT"/vmlinuz "$OUT"/initrd.img
{
	printf '{"version":"%s","files":[' "$VERSION"
	sep=""
	for f in filesystem.squashfs vmlinuz initrd.img; do
		printf '%s{"name":"%s","size":%s,"sha256":"%s"}' "$sep" "$f" "$(stat -c %s "$OUT/$f")" "$(sha256sum "$OUT/$f" | cut -d' ' -f1)"
		sep=","
	done
	printf ']}\n'
} > "$OUT/update.json"
cat "$OUT/update.json"
