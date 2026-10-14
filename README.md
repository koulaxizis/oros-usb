# orOS USB

A bootable USB image that starts straight into [orOS](https://useoros.online). MIT, free forever.

## What is inside

- Debian 13 (trixie) live system, built with live-build, with the usual non-free firmware for Wi-Fi, graphics and sound.
- [cage](https://github.com/cage-kiosk/cage), a kiosk Wayland compositor, showing only Debian's Chromium (no Google sign-in, sync, Safe Browsing or metrics; see `config/includes.chroot/etc/chromium/policies/managed/oros.json`).
- The [orOS Portable](https://github.com/koulaxizis/oros-portable) launcher serving orOS from `http://localhost:47800/`, the same address as orOS Portable, so its Dropbox sign-in works here too.
- UEFI with Secure Boot (Debian's signed shim and GRUB) and legacy BIOS boot.

## Using it

1. Write the ISO to a stick: on Windows with [Rufus](https://rufus.ie) (set "Persistent partition size" to keep your data), elsewhere with balenaEtcher or `dd`.
2. Without Rufus persistence, run `sudo scripts/add-persistence.sh /dev/sdX` to add a `persistence` partition in the rest of the stick. Without one, orOS still runs, but forgets everything on shutdown.
3. Boot from the stick. orOS opens full screen.

| Keys | |
|---|---|
| Ctrl+Alt+F2 | network setup (Wi-Fi) |
| Ctrl+Alt+F1 | back to orOS |
| Alt+Shift | switch keyboard English / Greek |
| Power button | shut down |

orOS lives in the home folder on the persistence partition (`~/oros/app`), so `/opt/oros/oros-portable update` (or a newer image) refreshes it.

## Building

`sudo scripts/build.sh <version>` on Debian 13, or let CI do it: `.github/workflows/build.yml` builds the ISO in a privileged `debian:trixie` container, writes it to a disk image with a persistence partition, boots it in QEMU with UEFI Secure Boot (Microsoft keys), and checks that orOS started and saved its profile to the persistence partition. Screenshots of the boot are kept as the `boot-test` artifact. `v*` tags publish a release.

## Not done yet

- A settings panel inside orOS for Wi-Fi, volume, brightness, time zone and power (a small local helper the launcher can talk to). For now Wi-Fi is on Ctrl+Alt+F2 and the time zone is UTC.
- Printing (CUPS) and Bluetooth.
- An installer to a hard disk.
