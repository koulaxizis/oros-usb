# orOS USB

A bootable USB image that starts straight into [orOS](https://useoros.online). MIT, free forever.

**Beta.** Releases are for testing. Known: on some PCs the very first start showed a white screen with a cursor, and a restart fixed it. The session watchdog (below) now restarts such a start by itself; System › Problem report shows each time it did.

## What is inside

- Debian 13 (trixie) live system, built with live-build, with the usual non-free firmware for Wi-Fi, graphics and sound.
- [cage](https://github.com/cage-kiosk/cage), a kiosk Wayland compositor, showing only Debian's Chromium (no Google sign-in, sync, Safe Browsing or metrics; see `config/includes.chroot/etc/chromium/policies/managed/oros.json`).
- The [orOS Portable](https://github.com/koulaxizis/oros-portable) launcher serving orOS from `http://localhost:47800/`, the same address as orOS Portable, so its Dropbox sign-in works here too.
- UEFI with Secure Boot (Debian's signed shim and GRUB) and legacy BIOS boot, with an orOS boot screen.
- A **System** button in orOS's top bar (the `system/extension` Chromium extension) for Wi-Fi, sound, battery, restart, shut down and installing orOS on the computer's disk. It talks to `oros-system` (`system/`), a small native messaging host that uses NetworkManager, PipeWire and systemd.

## Using it

1. Write the ISO to a stick: on Windows with [Rufus](https://rufus.ie) (set "Persistent partition size" to keep your data), elsewhere with balenaEtcher or `dd`.
2. Without Rufus persistence, run `sudo scripts/add-persistence.sh /dev/sdX` to add a `persistence` partition in the rest of the stick. Without one, orOS still runs, but forgets everything on shutdown.
3. Boot from the stick. orOS opens full screen. In the computer's boot menu the stick may appear under another name: Acer computers, for example, call every non-Windows USB stick "Linpus lite". That is the computer's label, not a different system; after installing, the entry reads "orOS".

The System button (Wi-Fi icon, top right) has Wi-Fi, sound, Bluetooth, printers, battery, screen brightness (laptops), time zone, Restart and Shut down. The **Internet** button next to it opens a normal browser window (tabs, address bar, DuckDuckGo search, the same no-Google settings, its own profile in `~/oros/data/web`) over orOS; closing it brings orOS back.

| Keys | |
|---|---|
| Ctrl+Alt+F2 | network setup in text mode (nmtui) |
| Ctrl+Alt+F1 | back to orOS |
| Alt+Shift | switch keyboard English / Greek |
| Power button | shut down |

System › **Disks** lists Downloads, the computer's own disks (Windows, another Linux) and USB sticks (FAT, exFAT, NTFS, ext4, Btrfs, XFS; partitions of 1 GiB or more, never the disk orOS runs from). **Open** mounts one read-only under `/media/oros/<label>` and shows it in the Internet window; **Close** unmounts it. orOS's own files are in its Files app. From a terminal: `sudo /opt/oros/oros-system mount --dev /dev/sdX1 [--unmount]`.

Saved Wi-Fi networks and their passwords are kept in `/home/.oros-network` (NetworkManager's keyfile folder, set in `/etc/NetworkManager/conf.d/oros.conf`), so they survive a restart; the installer copies them to the computer. System › **Problem report** writes `~/Downloads/orOS-report-<date>.txt` (hardware, disks, network state, this and the previous start's system log, and the logs below) and shows it in the Internet window, from where it can be attached to a message. The system log is kept across restarts in `/home/.oros-journal` (100 MB at most). Logs for when something goes wrong: `~/oros/data/session.log` (the kiosk), `~/oros/data/profile/chrome_debug.log` (the orOS window), `~/oros/data/web.log` (the Internet window) and `~/oros/data/oros-system.log` (the System button), `~/oros/data/install.log` and `update.log`.

**Session watchdog.** `oros-session` restarts orOS when its window shows no sign of life: the System extension pings `oros-system` every 10 s once the window has painted a frame, and the time lands in a file in RAM (`$XDG_RUNTIME_DIR/oros-alive-<uid>`). No ping within 60 s of a start, or none for 40 s later, stops the session and starts it again: the first time as it was, from the second time with the GPU off (`--disable-gpu`, also for the Internet window) for the rest of that boot. At most 3 restarts per boot. Each one is a `watchdog:` line in `session.log` and in the Problem report's first section. CI tests it with a fake compositor (`scripts/test-session-watchdog.sh`) and in QEMU with two starts that never ping (`WATCHDOG_TEST=2 scripts/test-boot.sh`); a normal boot test fails if the watchdog had to restart.

**Printing.** Driverless only, no vendor drivers: CUPS with cups-browsed and Avahi finds printers that support IPP Everywhere or AirPrint on the same network, and ipp-usb does the same for such printers on USB (almost every printer sold since about 2015). They show up by themselves in every print dialog (Ctrl+P in any app, or Print in orOS's apps) and in System › **Printers**, which also prints CUPS's test page. Queues are found again at every start, so nothing is stored.

**Bluetooth.** System › **Bluetooth** (shown when the computer has it) turns it on and off, finds devices for 10 seconds, pairs them (a keyboard shows the code to type on it), and connects, disconnects or forgets paired ones. Headphones and speakers play through PipeWire. Pairings are kept in `/home/.oros-bluetooth`, bound onto `/var/lib/bluetooth` by `oros-bluetooth.service`, and the installer copies them.

**Time.** The clock is set from the internet (systemd-timesyncd with Debian's `debian.pool.ntp.org` servers). System › **Time** picks the time zone (UTC until then); it is kept in `/home/.oros-timezone`, set again at every start by `oros-time.service`, and copied by the installer, whose screen offers the same choice. From a terminal: `sudo /opt/oros/oros-system time --zone Europe/Athens`. Computers that also run Windows keep local time in their hardware clock, so offline the clock can be off by the time zone's hours until the stick goes online. Brightness uses `brightnessctl`; each start begins at the computer's own level.

`~/oros/app` links to the orOS in the image; `/opt/oros/oros-portable update` replaces it with a newer copy on the persistence partition, and a newer image resets it.

### System updates

A new orOS USB is built and released every month (newer Chromium, kernel and firmware from Debian 13, and the latest orOS), after the same tests as every build. System › **System update** downloads its system (about 1.2 GB) onto the persistence partition, into slot A or B, whichever is not running, checks it against `update.json`, and writes `/oros-system/boot.cfg`. On the next start the boot menu (UEFI) starts the new system; the previous one and the stick's own system stay in the menu as a way back. Files and settings stay, because the persistence partition only keeps the home folder. Old BIOS computers keep starting the stick's own system. From a terminal: `sudo /opt/oros/oros-system update --yes`.

### Installing on the computer

System button → **Install orOS on this computer** erases the chosen internal disk and puts orOS on it: a FAT partition with the boot files and the system image (UEFI with Secure Boot), and an ext4 partition labelled `orosdisk` that keeps the home folder, optionally copied from the stick. The installed system starts like the stick, without it, and no longer offers to install. From a terminal: `sudo /opt/oros/oros-system install --disk /dev/sdX --yes`. UEFI only; old BIOS-only computers keep using the stick.

### Combo sticks (orOS USB + orOS Portable)

`sudo scripts/make-combo-sticks.sh ISO orOS-Portable-<version>.zip /dev/sdX [/dev/sdY ...]` writes sticks that do both: they boot orOS on any PC, and in Windows or Linux they show an NTFS drive `OROS` with [orOS Portable](https://github.com/koulaxizis/oros-portable), which runs without a restart. Layout: the image, then the NTFS partition, then the `persistence` partition (half of the free space by default, or `--persistence-gib N`). Each stick is read back and checked: the image byte for byte, the Portable files against `SHA256SUMS.txt`. 32 GB sticks or larger. The two modes keep separate data.

## Credits and licences

orOS USB (this repository: `oros-system`, the System button, the build scripts) is MIT, like [orOS](https://github.com/koulaxizis/oros) and the [orOS Portable](https://github.com/koulaxizis/oros-portable) launcher it runs. Everything else in the image is unmodified free software from [Debian](https://www.debian.org/), with thanks to everyone who makes it:

- [Debian 13](https://www.debian.org/) and the [Linux](https://kernel.org/) kernel (GPL-2.0), built into a live system with [live-build](https://salsa.debian.org/live-team/live-build), live-boot and live-config (GPL-3.0+, Debian Live project)
- [GRUB](https://www.gnu.org/software/grub/) (GPL-3.0+) and [shim](https://github.com/rhboot/shim) (BSD-2-Clause) in Debian's signed builds for Secure Boot, SYSLINUX (GPL-2.0+) for BIOS
- [cage](https://github.com/cage-kiosk/cage) (MIT) and [Chromium](https://www.chromium.org/) (BSD-3-Clause plus its components' licences, listed at `chrome://credits`), Debian's packages
- [CUPS](https://openprinting.github.io/cups/) and cups-browsed (Apache-2.0), [cups-filters](https://github.com/OpenPrinting/cups-filters) (GPL-2.0+, LGPL-2.1+ and MIT parts), [ipp-usb](https://github.com/OpenPrinting/ipp-usb) (BSD-2-Clause), [Ghostscript](https://www.ghostscript.com/) (AGPL-3.0+), [Avahi](https://avahi.org/) and nss-mdns (LGPL-2.1+), [BlueZ](https://www.bluez.org/) (GPL-2.0+ and LGPL-2.1+)
- [systemd](https://systemd.io/) (LGPL-2.1+, with systemd-timesyncd for the clock), the [IANA time zone database](https://www.iana.org/time-zones) (public domain), [brightnessctl](https://github.com/Hummer12007/brightnessctl) (MIT), [NetworkManager](https://networkmanager.dev/) (GPL-2.0+), wpa_supplicant (BSD-3-Clause), [PipeWire](https://pipewire.org/) and WirePlumber (MIT), ALSA (LGPL-2.1+), [Mesa](https://mesa3d.org/) (MIT), [Plymouth](https://gitlab.freedesktop.org/plymouth/plymouth) (GPL-2.0+), [ntfs-3g](https://github.com/tuxera/ntfs-3g) and exfatprogs (GPL-2.0+)
- [Noto](https://fonts.google.com/noto) fonts (SIL OFL 1.1)
- firmware and microcode from Debian's `firmware-*`, `bluez-firmware`, `amd64-microcode` and `intel-microcode` packages, under their vendors' licences (redistribution allowed; some are not free software)
- [Go](https://go.dev/) (BSD-3-Clause) compiles `oros-system` and the launcher

Every Debian package's licence is in `/usr/share/doc/<package>/copyright` on the system. Each build also carries, at the top of the stick and in `/usr/share/doc/oros/`:

- `CREDITS.txt`: the list above;
- `SOURCES.txt`: the commits of orOS, orOS Portable and orOS USB, and every Debian source package with its exact version and its [snapshot.debian.org](https://snapshot.debian.org/) address (`apt-get source <package>=<version>` fetches the same);
- `WRITTEN-OFFER.txt`: a three-year written offer of the complete source on a disc or stick at cost, for copies sold on sticks.

Releases attach the same `-sources.txt` and `-packages.txt` (every binary package and version). `scripts/sources-manifest.sh` writes them from the built system; CI checks they are on the ISO.

## Building

`sudo scripts/build.sh <version>` on Debian 13, or let CI do it: `.github/workflows/build.yml` builds the ISO in a privileged `debian:trixie` container, writes it to a disk image with a persistence partition, boots it in QEMU with UEFI Secure Boot (Microsoft keys), and checks that orOS started, the System button reached `oros-system`, and orOS saved its profile to the persistence partition. It then installs orOS from the ISO onto an empty disk image with `oros-system install` and boots that disk alone the same way (`scripts/test-install.sh`), and writes a combo stick image with the orOS Portable lite release and boots it (`scripts/test-combo-sticks.sh`), and puts the image's system onto a stick's persistence partition with `oros-system update` and boots it (`scripts/test-update.sh`). Tags, manual runs with a tag, and the monthly run (1st of the month) publish a release with the ISO and the update files. Screenshots of the boot are kept as the `boot-test` artifact. `v*` tags publish a release.

## Not done yet

- Printers that need a vendor driver, and adding a printer by its address.
