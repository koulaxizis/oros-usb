#!/usr/bin/env bash
# Writes the source manifest of an orOS USB build from its chroot:
#   <out>/packages.txt  every installed package: name, version, source, source version
#   <out>/SOURCES.txt   the exact source of every program in the image
# Usage: sources-manifest.sh <chroot> <out-dir> <version> <oros-usb-commit> <oros-commit> <oros-portable-commit>
set -euo pipefail
CHROOT="$1" OUT="$2" VERSION="$3" USB_COMMIT="$4" OROS_COMMIT="$5" PORTABLE_COMMIT="$6"
mkdir -p "$OUT"

dpkg-query --admindir="$CHROOT/var/lib/dpkg" -W \
	-f='${db:Status-Abbrev}\t${binary:Package}\t${Version}\t${source:Package}\t${source:Version}\n' |
	awk -F'\t' '$1 ~ /^ii/ { print $2 "\t" $3 "\t" $4 "\t" $5 }' | sort > "$OUT/packages.txt"
[ -s "$OUT/packages.txt" ] || { echo "no installed packages in $CHROOT" >&2; exit 1; }

{
cat <<TXT
orOS USB $VERSION: sources
==========================

Every program in this image is listed here with the exact source it was
built from. Licences: CREDITS.txt and /usr/share/doc/<package>/copyright.
Written offer for the source code: WRITTEN-OFFER.txt.

orOS USB (MIT), commit $USB_COMMIT
  https://github.com/koulaxizis/oros-usb/tree/$USB_COMMIT

orOS web app (MIT), commit $OROS_COMMIT
  https://github.com/koulaxizis/oros/tree/$OROS_COMMIT

orOS Portable launcher (MIT), commit $PORTABLE_COMMIT
  https://github.com/koulaxizis/oros-portable/tree/$PORTABLE_COMMIT

Debian 13 "trixie" source packages
----------------------------------

The binary packages come unmodified from Debian (trixie, trixie-updates,
trixie-security; main, contrib, non-free, non-free-firmware). Each source
package below can be fetched by its exact version:
  - with apt (deb-src lines for trixie):  apt-get source <package>=<version>
  - from https://snapshot.debian.org/package/<package>/<version>/
    (keeps every version Debian ever published; ":" written as %3A)
The list of binary packages in the image, with their versions, is in
packages.txt (/usr/share/doc/oros/packages.txt on the running system).

TXT
cut -f3,4 "$OUT/packages.txt" | sort -u | while IFS=$'\t' read -r src ver; do
	printf '%s %s  https://snapshot.debian.org/package/%s/%s/\n' "$src" "$ver" "$src" "${ver//:/%3A}"
done
} > "$OUT/SOURCES.txt"
