package main

// Night Light: orOS (nightlight.js) decides when and how warm the screen
// is; on this stick the screen's own colours change instead of orOS's CSS
// filter, so the Internet window and Tor Browser turn warm too. labwc
// offers wlr-gamma-control, and wlsunset (Debian) sets the ramps. The
// gamma only holds while its client runs, so one wlsunset runs in the
// background (Setsid, it outlives this host) with both its day and night
// temperatures at the asked value (-t k -T k+1: wlsunset needs high > low),
// which makes it a steady colour; orOS sends a new value for its fades.
// The page asks with "night.set" {value: kelvin}, 0 = back to normal.
//
// A compositor without wlr-gamma-control makes wlsunset quit at once. Then this
// boot is marked (nightNoGammaFile), status stops reporting "night", the
// extension removes data-oros-sys-night and orOS draws its own filter
// again. The boot test logs whether gamma works (selftestDeskMain).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	nightMin     = 1000
	nightNeutral = 6500
)

// nightAvailable: wlsunset is installed, we run inside a Wayland session,
// and this boot has not found the compositor without gamma control.
func nightAvailable() bool {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	if _, err := os.Stat(nightNoGammaFile()); err == nil {
		return false
	}
	_, err := exec.LookPath("wlsunset")
	return err == nil
}

func nightPidFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "oros-night-"+strconv.Itoa(os.Getuid())+".pid")
}

// nightNoGammaFile lives in RAM ($XDG_RUNTIME_DIR), so a new boot (or a
// newer image) tries again.
func nightNoGammaFile() string {
	return strings.TrimSuffix(nightPidFile(), ".pid") + ".nogamma"
}

// nightArgs is the wlsunset command line for a temperature, or nil for
// "normal colours" (0, or 6500 K and above). Out-of-range values are clamped.
func nightArgs(k int) []string {
	if k <= 0 || k >= nightNeutral {
		return nil
	}
	if k < nightMin {
		k = nightMin
	}
	return []string{"-t", strconv.Itoa(k), "-T", strconv.Itoa(k + 1), "-S", "06:00", "-s", "18:00", "-d", "60"}
}

// nightStop ends the wlsunset this host started earlier, and only that
// process (its pid file and /proc/<pid>/comm must agree).
func nightStop() {
	b, err := os.ReadFile(nightPidFile())
	if err != nil {
		return
	}
	_ = os.Remove(nightPidFile())
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil || strings.TrimSpace(string(comm)) != "wlsunset" {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

func nightSet(k int) reply {
	if !nightAvailable() {
		return reply{"ok": false, "error": "not available"}
	}
	nightStop()
	args := nightArgs(k)
	if args == nil {
		return reply{"ok": true, "kelvin": 0}
	}
	cmd := exec.Command("wlsunset", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("night.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		// It quit at once: no gamma control in this compositor.
		_ = os.WriteFile(nightNoGammaFile(), []byte("1"), 0o600)
		logf("night: wlsunset quit at once, no gamma control; orOS keeps its own filter")
		return reply{"ok": false, "error": "no gamma control", "night": false}
	case <-time.After(1500 * time.Millisecond):
	}
	_ = os.WriteFile(nightPidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	return reply{"ok": true, "kelvin": k}
}
