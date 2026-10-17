# orOS USB

A bootable USB image that starts straight into [orOS](https://useoros.online). MIT, free forever.

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

The System button (Wi-Fi icon, top right) has Wi-Fi, sound, battery, Restart and Shut down. The **Internet** button next to it opens a normal browser window (tabs, address bar, DuckDuckGo search, the same no-Google settings, its own profile in `~/oros/data/web`) over orOS; closing it brings orOS back.

| Keys | |
|---|---|
| Ctrl+Alt+F2 | network setup in text mode (nmtui) |
| Ctrl+Alt+F1 | back to orOS |
| Alt+Shift | switch keyboard English / Greek |
| Power button | shut down |

`~/oros/app` links to the orOS in the image; `/opt/oros/oros-portable update` replaces it with a newer copy on the persistence partition, and a newer image resets it.

### System updates

A new orOS USB is built and released every month (newer Chromium, kernel and firmware from Debian 13, and the latest orOS), after the same tests as every build. System › **System update** downloads its system (about 1.2 GB) onto the persistence partition, into slot A or B, whichever is not running, checks it against `update.json`, and writes `/oros-system/boot.cfg`. On the next start the boot menu (UEFI) starts the new system; the previous one and the stick's own system stay in the menu as a way back. Files and settings stay, because the persistence partition only keeps the home folder. Old BIOS computers keep starting the stick's own system. From a terminal: `sudo /opt/oros/oros-system update --yes`.

### Installing on the computer

System button → **Install orOS on this computer** erases the chosen internal disk and puts orOS on it: a FAT partition with the boot files and the system image (UEFI with Secure Boot), and an ext4 partition labelled `orosdisk` that keeps the home folder, optionally copied from the stick. The installed system starts like the stick, without it. From a terminal: `sudo /opt/oros/oros-system install --disk /dev/sdX --yes`. UEFI only; old BIOS-only computers keep using the stick.

### Combo sticks (orOS USB + orOS Portable)

`sudo scripts/make-combo-sticks.sh ISO orOS-Portable-<version>.zip /dev/sdX [/dev/sdY ...]` writes sticks that do both: they boot orOS on any PC, and in Windows or Linux they show an NTFS drive `OROS` with [orOS Portable](https://github.com/koulaxizis/oros-portable), which runs without a restart. Layout: the image, then the NTFS partition, then the `persistence` partition (half of the free space by default, or `--persistence-gib N`). Each stick is read back and checked: the image byte for byte, the Portable files against `SHA256SUMS.txt`. 32 GB sticks or larger. The two modes keep separate data.

## Building

`sudo scripts/build.sh <version>` on Debian 13, or let CI do it: `.github/workflows/build.yml` builds the ISO in a privileged `debian:trixie` container, writes it to a disk image with a persistence partition, boots it in QEMU with UEFI Secure Boot (Microsoft keys), and checks that orOS started, the System button reached `oros-system`, and orOS saved its profile to the persistence partition. It then installs orOS from the ISO onto an empty disk image with `oros-system install` and boots that disk alone the same way (`scripts/test-install.sh`), and writes a combo stick image with the orOS Portable lite release and boots it (`scripts/test-combo-sticks.sh`), and puts the image's system onto a stick's persistence partition with `oros-system update` and boots it (`scripts/test-update.sh`). Tags, manual runs with a tag, and the monthly run (1st of the month) publish a release with the ISO and the update files. Screenshots of the boot are kept as the `boot-test` artifact. `v*` tags publish a release.

## Not done yet

- Brightness and time zone in the System button (the time zone is UTC).
- Printing (CUPS) and Bluetooth.
