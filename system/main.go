// oros-system is the bridge between orOS and the computer on the orOS USB
// image. Chromium starts it as a native messaging host for the "orOS
// system" extension (Wi-Fi, sound, battery, power, disks, install, update), and
// root runs it as "oros-system install" to put orOS on an internal disk and
// as "oros-system update" to download a new system and "oros-system mount"
// to open another disk read-only.
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
	fmt.Fprintln(os.Stderr, "usage: oros-system install --disk /dev/sdX [--yes] | update [--yes] | mount --dev /dev/sdX1 [--unmount]  (or run by Chromium)")
	os.Exit(2)
}
