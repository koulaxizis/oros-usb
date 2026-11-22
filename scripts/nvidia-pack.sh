#!/usr/bin/env bash
# NVIDIA's own driver as a separate download (wave G5, 2026-11-22; Chris:
# «υποστήριξη και για κάρτες NVIDIA», also older ones). Built for the exact
# kernel of this image, from Debian's non-free packages (the 550 branch in
# Debian 13: GeForce 700/900/10/16/20/30/40 series and newer; Kepler cards
# that need the 470 branch stay on Mesa's nouveau, which Debian 13 no longer
# builds 470 for), as a systemd-sysext image that oros-system merges over
# /usr at boot (system/nvidia.go). The image itself carries only the small
# description written to /opt/oros/nvidia.json, so computers without an
# NVIDIA card download nothing.
#
# The kernel modules are signed for UEFI Secure Boot with the orOS key: the
# private key comes from the NVIDIA_MOK_KEY secret (PEM) and its certificate
# from NVIDIA_MOK_CERT (PEM). Without them (pull requests, forks) a key is
# made for this build only, so the pack works with Secure Boot off and in
# the tests, and nvidia.json says "ephemeral": true.
#
#   scripts/nvidia-pack.sh CHROOT VERSION OUT_DIR OPT_DIR
#     -> OUT_DIR/orOS-USB-VERSION-nvidia.raw, OPT_DIR/nvidia.json,
#        OPT_DIR/oros-mok.der (the certificate people enrol)
set -euo pipefail

CHROOT="$(realpath "$1")"
VERSION="$2"
OUT="$(mkdir -p "$3" && realpath "$3")"
OPT="$4"
NAME="orOS-USB-$VERSION-nvidia.raw"
# Next to the chroot, not in /tmp: a container's /tmp is often itself an
# overlay, which cannot be an overlay's upper folder.
W="$(mktemp -d -p "$(dirname "$CHROOT")" .nvidia-pack.XXXXXX)"
cleanup() {
	for m in dev/pts dev proc sys; do umount -l "$W/root/$m" 2>/dev/null || true; done
	umount -l "$W/root" 2>/dev/null || true
	umount -l "$W/t" 2>/dev/null || true
	rm -rf "$W"
}
trap cleanup EXIT

# The level the image's os-release carries too (build.sh), so the pack is
# merged only into the system it was built for.
sysext_level() { echo "orOS-$1" | tr -c 'A-Za-z0-9._\n' '_'; }

KVER="$(ls "$CHROOT/usr/lib/modules" | sort -V | tail -1)"
echo "kernel $KVER"

# The key the modules are signed with.
if [ -n "${NVIDIA_MOK_KEY:-}" ] && [ -n "${NVIDIA_MOK_CERT:-}" ]; then
	printf '%s\n' "$NVIDIA_MOK_KEY" > "$W/mok.key"
	printf '%s\n' "$NVIDIA_MOK_CERT" > "$W/mok.pem"
	EPHEMERAL=false
else
	echo "NVIDIA_MOK_KEY/NVIDIA_MOK_CERT not set: a key for this build only"
	openssl req -new -x509 -newkey rsa:2048 -nodes -days 36500 -subj "/CN=orOS test build module key/" \
		-addext "extendedKeyUsage=codeSigning,1.3.6.1.4.1.311.10.3.6" \
		-keyout "$W/mok.key" -out "$W/mok.pem" 2>/dev/null
	EPHEMERAL=true
fi
chmod 600 "$W/mok.key"
openssl x509 -in "$W/mok.pem" -outform DER -out "$W/mok.der"

# A throwaway copy of the finished system (overlay: nothing written to it
# reaches the image) where Debian's packages are installed and DKMS builds
# the modules for the image's kernel.
mkdir -p "$W/up" "$W/work" "$W/root"
if ! mount -t overlay overlay -o "lowerdir=$CHROOT,upperdir=$W/up,workdir=$W/work" "$W/root"; then
	echo "overlay on $(dirname "$CHROOT") refused: upper folder in memory"
	mkdir -p "$W/t"
	mount -t tmpfs -o size=8G tmpfs "$W/t"
	mkdir -p "$W/t/up" "$W/t/work"
	mount -t overlay overlay -o "lowerdir=$CHROOT,upperdir=$W/t/up,workdir=$W/t/work" "$W/root"
fi
for m in proc sys dev dev/pts; do mkdir -p "$W/root/$m"; mount --bind "/$m" "$W/root/$m"; done
cp /etc/resolv.conf "$W/root/etc/resolv.conf"
# DKMS signs with /var/lib/dkms/mok.key when it is there.
mkdir -p "$W/root/var/lib/dkms"
cp "$W/mok.key" "$W/root/var/lib/dkms/mok.key"
cp "$W/mok.der" "$W/root/var/lib/dkms/mok.pub"
chroot "$W/root" dpkg-query -W -f '${Package}\n' | sort > "$W/before"
PKGS="linux-headers-$KVER nvidia-kernel-dkms nvidia-driver-libs nvidia-vulkan-icd nvidia-egl-icd libnvidia-egl-wayland1 libnvidia-egl-gbm1 firmware-nvidia-gsp nvidia-smi nvidia-kernel-support nvidia-modprobe"
# Debian's non-free area, whatever the build left in the copy's sources.
cat > "$W/root/etc/apt/sources.list.d/oros-nvidia.sources" <<'SRC'
Types: deb
URIs: http://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main contrib non-free non-free-firmware

Types: deb
URIs: http://deb.debian.org/debian-security
Suites: trixie-security
Components: main contrib non-free non-free-firmware
SRC
chroot "$W/root" env DEBIAN_FRONTEND=noninteractive apt-get update -q
chroot "$W/root" env DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends $PKGS
# DKMS builds for every kernel with headers when the package goes in; ask
# again for this one in case the copy's running kernel confused it.
chroot "$W/root" dkms autoinstall -k "$KVER" || true
DRIVER="$(chroot "$W/root" dpkg-query -W -f '${Version}' nvidia-kernel-dkms | sed 's/-.*//')"
echo "NVIDIA driver $DRIVER"
MODDIR="usr/lib/modules/$KVER/updates/dkms"
ls "$W/root/$MODDIR"/nvidia*.ko* >/dev/null
for ko in "$W/root/$MODDIR"/*.ko*; do
	case "$ko" in *.xz) xz -d "$ko"; ko="${ko%.xz}" ;; *.zst) zstd -q -d --rm "$ko"; ko="${ko%.zst}" ;; esac
	# signed by DKMS with our key; signed again here so it never depends on
	# DKMS's settings
	"$W/root/usr/src/linux-headers-$KVER/scripts/sign-file" sha256 "$W/mok.key" "$W/mok.der" "$ko" 2>/dev/null ||
		"$(ls "$W"/root/usr/lib/linux-kbuild-*/scripts/sign-file | head -1)" sha256 "$W/mok.key" "$W/mok.der" "$ko"
	modinfo -F signer "$ko" | grep -q . || { echo "$ko is not signed" >&2; exit 1; }
done
chroot "$W/root" depmod "$KVER"

# What goes into the pack: every file of the newly installed packages that
# come from NVIDIA's driver (and its EGL platform libraries), under /usr,
# the modules and the module index of this kernel. Compilers and headers
# that DKMS needed stay out.
chroot "$W/root" dpkg-query -W -f '${Package} ${source:Package}\n' | sort > "$W/after"
awk 'NR==FNR { seen[$1]=1; next } !($1 in seen) && ($2 ~ /^(nvidia-graphics-drivers|egl-wayland|egl-gbm|nvidia-modprobe|firmware-nonfree)$/) { print $1 }' \
	"$W/before" "$W/after" > "$W/pkgs"
cat "$W/pkgs"
grep -qx nvidia-driver-libs "$W/pkgs"
mkdir -p "$W/ext"
while read -r p; do
	chroot "$W/root" dpkg -L "$p"
done < "$W/pkgs" | sort -u | while read -r f; do
	case "$f" in /usr/src/* | /usr/lib/modules/* | /lib/modules/*) continue ;; esac
	src="$W/root$f"
	[ -f "$src" ] || [ -L "$src" ] || continue
	# Debian 13 has a merged /usr: /lib, /bin and /sbin are /usr's. A
	# system extension covers /usr only, so /etc files stay out (oros-system
	# writes the module options to /run/modprobe.d itself).
	case "$f" in
	/usr/*) dst="$f" ;;
	/lib/* | /bin/* | /sbin/*) dst="/usr$f" ;;
	*) continue ;;
	esac
	mkdir -p "$W/ext$(dirname "$dst")"
	cp -a "$src" "$W/ext$dst"
done
mkdir -p "$W/ext/$MODDIR"
cp -a "$W/root/$MODDIR"/*.ko "$W/ext/$MODDIR/"
for f in "$W/root/usr/lib/modules/$KVER"/modules.*; do cp -a "$f" "$W/ext/usr/lib/modules/$KVER/"; done
# Licence texts stay with the files.
mkdir -p "$W/ext/usr/share/doc/oros-nvidia"
while read -r p; do
	[ -f "$W/root/usr/share/doc/$p/copyright" ] && cp "$W/root/usr/share/doc/$p/copyright" "$W/ext/usr/share/doc/oros-nvidia/$p.copyright"
done < "$W/pkgs"
# Debian names the modules nvidia-current*.ko; the usual names point to
# them, and the options go to the real names (kmod reads /usr/lib/modprobe.d,
# which the pack brings).
mkdir -p "$W/ext/usr/lib/modprobe.d"
{
	echo "# orOS NVIDIA pack (scripts/nvidia-pack.sh): Debian's module names"
	for ko in "$W/ext/$MODDIR"/nvidia*.ko; do
		m="$(basename "$ko" .ko)"
		short="${m/nvidia-current/nvidia}"
		[ "$short" = "$m" ] || echo "alias $short $m"
		case "$short" in
		nvidia) echo "options $m NVreg_PreserveVideoMemoryAllocations=1" ;;
		nvidia-drm) echo "options $m modeset=1 fbdev=1" ;;
		esac
	done
} > "$W/ext/usr/lib/modprobe.d/oros-nvidia.conf"
cat "$W/ext/usr/lib/modprobe.d/oros-nvidia.conf"
grep -q '^options .* modeset=1' "$W/ext/usr/lib/modprobe.d/oros-nvidia.conf"
# Which cards this driver supports, for the System button's check: NVIDIA's
# own list (PCI device ids with no "legacy" branch) where Debian ships it,
# otherwise Debian's list for this driver (the .ids file nvidia-detect reads).
echo "lists of supported cards:"
find "$W/root/usr" \( -iname 'supported-gpus.json*' -o -iname '*.ids' \) -path '*nvidia*' | sed "s|$W/root||"
SUP="$(find "$W/root/usr" -iname 'supported-gpus.json*' | head -1)"
IDS="$(find "$W/root/usr" -iname '*.ids' -path '*nvidia*' | grep -i -e current -e '/nvidia.ids$' | grep -v -i legacy | head -1 || true)"
if [ -n "$SUP" ]; then
	case "$SUP" in *.gz) zcat "$SUP" > "$W/sup.json" ;; *) cp "$SUP" "$W/sup.json" ;; esac
	jq -c '[.chips[] | select((.legacybranch // "") == "") | .devid | ascii_downcase] | unique' "$W/sup.json" > "$W/devids"
elif [ -n "$IDS" ]; then
	# "10de 1c8d", "10DE1C8D" or "0x1c8d": the four hex digits of each device
	grep -v '^#' "$IDS" | tr 'A-F' 'a-f' | grep -o -E '(10de[^0-9a-f]?|0x)[0-9a-f]{4}' | grep -o -E '[0-9a-f]{4}$' |
		sed 's/^/0x/' | sort -u | jq -R . | jq -sc . > "$W/devids"
else
	echo "no list of supported cards" >&2
	exit 1
fi
test "$(jq length "$W/devids")" -gt 100
# systemd-sysext merges the pack only into the system it was built for.
mkdir -p "$W/ext/usr/lib/extension-release.d"
printf 'ID=debian\nSYSEXT_LEVEL=%s\n' "$(sysext_level "$VERSION")" \
	> "$W/ext/usr/lib/extension-release.d/extension-release.oros-nvidia"

rm -f "$OUT/$NAME"
mksquashfs "$W/ext" "$OUT/$NAME" -comp zstd -all-root -quiet -no-progress
SIZE="$(stat -c %s "$OUT/$NAME")"
SUM="$(sha256sum "$OUT/$NAME" | cut -d' ' -f1)"
mkdir -p "$OPT"
cp "$W/mok.der" "$OPT/oros-mok.der"
# The exact Debian sources the pack was made from (non-free; the source
# packages stay on Debian's mirrors and snapshot.debian.org).
while read -r p; do
	chroot "$W/root" dpkg-query -W -f '${Package} ${Version} source ${source:Package} ${source:Version}\n' "$p"
done < "$W/pkgs" > "$OPT/nvidia-sources.txt"
jq -n --arg file "$NAME" --argjson size "$SIZE" --arg sha "$SUM" --arg driver "$DRIVER" \
	--arg kernel "$KVER" --argjson devids "$(cat "$W/devids")" --argjson eph "$EPHEMERAL" \
	--arg url "https://github.com/koulaxizis/oros-usb/releases/download/v$VERSION/$NAME" \
	'{file: $file, size: $size, sha256: $sha, driver: $driver, kernel: $kernel, url: $url, ephemeral: $eph, devids: $devids}' \
	> "$OPT/nvidia.json"
echo "NVIDIA pack $NAME: $((SIZE / 1048576)) MiB, driver $DRIVER, $(jq length "$W/devids") cards"
