package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Time zone and brightness for the System button. The live system keeps
// only /home, so the chosen time zone is saved in /home/.oros-timezone,
// next to the home folder like the saved networks: it survives a restart,
// the installer copies it, and "oros-system time --apply" (oros-time.service)
// sets it at every start. The clock itself comes from NTP
// (systemd-timesyncd, Debian's pool servers).

var (
	zoneFile  = "/home/.oros-timezone"
	zoneinfo  = "/usr/share/zoneinfo"
	localtime = "/etc/localtime"
	etcZone   = "/etc/timezone"
)

var zoneName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)

// validZone accepts only a real zone file under zoneinfo, so nothing else
// can be linked to /etc/localtime.
func validZone(z string) bool {
	if !zoneName.MatchString(z) || strings.Contains(z, "..") {
		return false
	}
	b := make([]byte, 4)
	f, err := os.Open(filepath.Join(zoneinfo, z))
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return false
	}
	_, err = f.Read(b)
	return err == nil && string(b) == "TZif"
}

// zones lists the canonical zones (zone1970.tab), sorted, UTC first.
func zones() []string {
	out := []string{"UTC"}
	f, err := os.Open(filepath.Join(zoneinfo, "zone1970.tab"))
	if err != nil {
		return out
	}
	defer f.Close()
	var list []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if c := strings.Split(line, "\t"); len(c) >= 3 && zoneName.MatchString(c[2]) {
			list = append(list, c[2])
		}
	}
	sort.Strings(list)
	return append(out, list...)
}

// currentZone reads the zone /etc/localtime links to.
func currentZone() string {
	t, err := os.Readlink(localtime)
	if err != nil {
		return "UTC"
	}
	if i := strings.Index(t, "zoneinfo/"); i >= 0 {
		t = t[i+len("zoneinfo/"):]
	}
	if t == "Etc/UTC" || !zoneName.MatchString(t) {
		return "UTC"
	}
	return t
}

// applyZone points /etc/localtime at the zone; Chromium follows the change.
func applyZone(z string) error {
	tmp := localtime + ".oros"
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Join(zoneinfo, z), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, localtime); err != nil {
		return err
	}
	return os.WriteFile(etcZone, []byte(z+"\n"), 0o644)
}

func setZone(z string) error {
	if !validZone(z) {
		return fmt.Errorf("unknown time zone %q", z)
	}
	tmp := zoneFile + ".new"
	if err := os.WriteFile(tmp, []byte(z+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, zoneFile); err != nil {
		return err
	}
	return applyZone(z)
}

// savedZone is the zone saved on the stick, or "" when none (or a broken one).
func savedZone() string {
	b, err := os.ReadFile(zoneFile)
	if err != nil {
		return ""
	}
	z := strings.TrimSpace(string(b))
	if !validZone(z) {
		return ""
	}
	return z
}

// timeMain: "oros-system time --zone Europe/Athens" saves and sets a zone,
// "oros-system time --apply" sets the saved one (at boot).
func timeMain(args []string) int {
	if len(args) == 2 && args[0] == "--zone" {
		if err := setZone(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "--apply" {
		z := savedZone()
		if z == "" {
			return 0
		}
		if err := applyZone(z); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: oros-system time --zone <Area/City> | --apply")
	return 2
}

func timeGet() reply {
	res := reply{"ok": true, "zone": currentZone(), "zones": zones()}
	if out, err := run("timedatectl", "show", "-p", "NTPSynchronized", "--value"); err == nil {
		res["synced"] = strings.TrimSpace(out) == "yes"
	}
	return res
}

func timeZone(z string) reply {
	if !validZone(z) {
		return reply{"ok": false, "error": "unknown time zone"}
	}
	if out, err := rootRun("time", "--zone", z); err != nil {
		return fail(err, out)
	}
	return reply{"ok": true, "zone": z}
}

// Brightness goes through brightnessctl (the oros user is in the video
// group, which udev lets write the backlight). "-m" prints
// "device,class,current,percent%,max".

func parseBrightness(out string) (int, bool) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		c := strings.Split(line, ",")
		if len(c) < 5 || c[1] != "backlight" {
			continue
		}
		cur, err1 := strconv.Atoi(c[2])
		max, err2 := strconv.Atoi(c[4])
		if err1 != nil || err2 != nil || max <= 0 {
			continue
		}
		return (cur*100 + max/2) / max, true
	}
	return 0, false
}

func brightness() (int, bool) {
	out, err := run("brightnessctl", "-m", "-c", "backlight", "info")
	if err != nil {
		return 0, false
	}
	return parseBrightness(out)
}

func setBrightness(v int) reply {
	// Never fully dark: a black screen looks like a broken computer.
	if v < 5 {
		v = 5
	}
	if v > 100 {
		v = 100
	}
	if _, ok := brightness(); !ok {
		return fail(errors.New("no backlight"), "")
	}
	out, err := run("brightnessctl", "-q", "-c", "backlight", "set", strconv.Itoa(v)+"%")
	if err != nil {
		return fail(err, out)
	}
	return reply{"ok": true}
}
