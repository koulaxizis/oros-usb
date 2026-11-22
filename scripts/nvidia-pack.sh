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
W="$(mktemp -d)"
cleanup() {
	for m in dev/pts dev proc sys; do umount -l "$W/root/$m" 2>/dev/null || true; done
	umount -l "$W/root" 2>/dev/null || true
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
mount -t overlay overlay -o "lowerdir=$CHROOT,upperdir=$W/up,workdir=$W/work" "$W/root"
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
# Which cards this driver supports (NVIDIA's own list: PCI device ids
# with no "legacy" branch), for the System button's check.
SUP="$(find "$W/root/usr/share" -name supported-gpus.json | head -1)"
[ -n "$SUP" ] || { echo "no supported-gpus.json" >&2; exit 1; }
jq -c '[.chips[] | select((.legacybranch // "") == "") | .devid | ascii_downcase] | unique' "$SUP" > "$W/devids"
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
