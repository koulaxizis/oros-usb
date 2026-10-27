package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The System button's problem report: one text file in Downloads with what
// is needed to find out why a start or an install went wrong (this and the
// previous start's system log, orOS's own logs, the hardware). It holds no
// passwords; network names and the computer's hardware are in it.

// reportCommands are run in order; a missing tool just leaves a note.
var reportCommands = [][]string{
	{"cat", "/opt/oros/version"},
	{"uname", "-a"},
	{"cat", "/proc/cmdline"},
	{"uptime"},
	{"timedatectl"},
	{"free", "-m"},
	{"lspci", "-nnk"},
	{"lsusb"},
	{"lsblk", "-o", "NAME,SIZE,TYPE,FSTYPE,LABEL,MOUNTPOINT,TRAN,MODEL"},
	{"df", "-h"},
	{"nmcli", "general"},
	{"nmcli", "device"},
	{"lpstat", "-t"},
	{"rfkill"},
	{"bluetoothctl", "show"},
	{"journalctl", "--list-boots", "--no-pager"},
	{"journalctl", "-b", "-1", "--no-pager", "-o", "short-monotonic", "-n", "1500"},
	{"journalctl", "-b", "0", "--no-pager", "-o", "short-monotonic", "-n", "1500"},
}

// reportLogs are orOS's own logs in ~/oros/data, with how many last lines.
var reportLogs = []struct {
	name  string
	lines int
}{
	{"session.log", 300},
	{"launcher.log", 200},
	{"install.log", 200},
	{"update.log", 200},
	{"oros-system.log", 200},
	{"web.log", 200},
	{"profile/chrome_debug.log", 400},
	{"web/chrome_debug.log", 200},
}

func writeReport(home string) (string, error) {
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "orOS problem report %s\n", time.Now().Format(time.RFC3339))
	data := filepath.Join(home, "oros", "data")
	// First, what the session watchdog did (a window that never painted).
	b.WriteString("\n===== recoveries (session watchdog)\n")
	b.WriteString(watchdogLines(filepath.Join(data, "session.log")))
	for _, c := range reportCommands {
		fmt.Fprintf(&b, "\n===== %s\n", strings.Join(c, " "))
		out, err := runFor(20*time.Second, c[0], c[1:]...)
		b.WriteString(out)
		if err != nil {
			fmt.Fprintf(&b, "(%v)\n", err)
		}
	}
	for _, l := range reportLogs {
		fmt.Fprintf(&b, "\n===== %s\n", l.name)
		raw, err := os.ReadFile(filepath.Join(data, l.name))
		if err != nil {
			b.WriteString("(none)\n")
			continue
		}
		b.WriteString(lastLines(string(raw), l.lines))
	}
	p := filepath.Join(dir, "orOS-report-"+time.Now().Format("2006-01-02-150405")+".txt")
	return p, os.WriteFile(p, []byte(b.String()), 0o644)
}

// watchdogLines picks the watchdog's lines out of session.log: each start
// that showed no sign of life and how the session was started again.
func watchdogLines(log string) string {
	raw, err := os.ReadFile(log)
	if err != nil {
		return "(no session.log)\n"
	}
	var b strings.Builder
	for _, l := range strings.SplitAfter(string(raw), "\n") {
		if strings.Contains(l, "watchdog") {
			b.WriteString(l)
		}
	}
	if b.Len() == 0 {
		return "(none)\n"
	}
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func lastLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > n+1 {
		lines = lines[len(lines)-n-1:]
	}
	out := strings.Join(lines, "")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// report writes the file and shows it in the Internet window.
func report() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	p, err := writeReport(home)
	if err != nil {
		return fail(err, "")
	}
	r := openWeb("file://" + p)
	r["file"] = filepath.Base(p)
	return r
}
