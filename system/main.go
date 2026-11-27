// oros-system is the bridge between orOS and the computer on the orOS USB
// image. Chromium starts it as a native messaging host for the "orOS
// system" extension (Wi-Fi, sound, battery, power, disks, install, update), and
// root runs it as "oros-system install" to put orOS on an internal disk and
// as "oros-system update" to download a new system and "oros-system mount"
// to open another disk (Windows disks read-only), "oros-system disks"
// lists them as JSON, "oros-system time" to set the time zone, and
// "oros-system vault-base" to make the folder for encrypted users (vault.go),
// "oros-system ai" sets up and starts the local AI (ai.go), and
// "oros-system claude" downloads Anthropic's Claude desktop (claude.go),
// "oros-system gpu" lists the graphics cards and, with --apply, puts games
// on a laptop's second card (gpu.go), "oros-system soft" installs and
// updates the software center's programs from Flathub (soft.go) and root's
// "oros-system addons --apply" its browser add-ons (addons.go), and
// "oros-system proton --update" brings the newest GE-Proton into Steam
// (proton.go).
// "keyboard", "idle", "screen", "display" and "reader" are the session's
// desktop parts (desk.go, display.go): keyboard layouts, the screen going
// off and sleep when idle, second screens, and the Orca screen reader.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "install" {
		os.Exit(installMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "update" {
		os.Exit(updateMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "time" {
		os.Exit(timeMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "tor" {
		os.Exit(torMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "claude" {
		os.Exit(claudeMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "nvidia" {
		os.Exit(nvidiaMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "gpu" {
		os.Exit(gpuMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "console" {
		os.Exit(consoleMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "soft" {
		os.Exit(softMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "proton" {
		os.Exit(protonMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "addons" {
		os.Exit(addonsMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "ai" {
		os.Exit(aiMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "selftest-files" {
		os.Exit(selftestFilesMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "vault-base" {
		os.Exit(vaultBaseMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "selftest-vault" {
		os.Exit(selftestVaultMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "disks" {
		os.Exit(disksMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "keyboard" {
		os.Exit(keyboardMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "idle" {
		os.Exit(idleMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "screen" {
		os.Exit(screenMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "display" {
		os.Exit(displayMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "reader" {
		os.Exit(readerMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "selftest-desk" {
		os.Exit(selftestDeskMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "mount" {
		os.Exit(mountMain(os.Args[2:]))
	}
	// Chromium passes the extension's origin as the first argument.
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "chrome-extension://") {
		if err := serveHost(os.Stdin, os.Stdout); err != nil {
			logf("host: %v", err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "usage: oros-system install --disk /dev/sdX [--yes] | update [--yes] | mount --dev /dev/sdX1 [--unmount] | disks | time --zone Area/City | time --apply | gpu [--apply] | nvidia [--install|--remove|--boot|--auto] | soft --install|--remove --id ID|--update|--auto | proton --update | addons --apply | ai --fetch [--model M] | ai --serve | claude --fetch | keyboard --layouts|--options | idle | screen --off|--on | display --apply | reader --enabled|--session  (or run by Chromium)")
	os.Exit(2)
}
