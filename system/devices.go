package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Printers and Bluetooth for the System button.
//
// Printing is driverless only: CUPS with cups-browsed and Avahi finds IPP
// Everywhere and AirPrint printers on the network, ipp-usb makes USB
// printers of the same kind look like network ones. No vendor drivers.
// Queues are found again at every start, so nothing needs keeping.
//
// Bluetooth is BlueZ through bluetoothctl. Pairings live in
// /home/.oros-bluetooth (bound onto /var/lib/bluetooth by
// oros-bluetooth.service), so they survive a restart and the installer
// copies them.

var btDir = "/home/.oros-bluetooth"

// runFor is run with a time limit: bluetoothctl waits for ever when
// bluetoothd is not there.
func runFor(d time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|[\x01\x02\r]`)

// cleanBT removes bluetoothctl's colours and prompt marks.
func cleanBT(s string) string { return ansi.ReplaceAllString(s, "") }

var macRe = regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`)

type btDevice struct {
	Mac       string `json:"mac"`
	Name      string `json:"name"`
	Paired    bool   `json:"paired,omitempty"`
	Connected bool   `json:"connected,omitempty"`
}

// parseBTDevices reads "Device AA:BB:CC:DD:EE:FF Name" lines. A device
// without a name repeats its address with dashes; those are left out
// unless keepUnnamed.
func parseBTDevices(out string, keepUnnamed bool) []btDevice {
	var list []btDevice
	seen := map[string]bool{}
	for _, line := range strings.Split(cleanBT(out), "\n") {
		i := strings.Index(line, "Device ")
		if i < 0 {
			continue
		}
		f := strings.SplitN(strings.TrimSpace(line[i+len("Device "):]), " ", 2)
		if len(f) == 0 || !macRe.MatchString(f[0]) || seen[f[0]] {
			continue
		}
		name := ""
		if len(f) == 2 {
			name = strings.TrimSpace(f[1])
		}
		if name == "" || name == strings.ReplaceAll(f[0], ":", "-") {
			if !keepUnnamed {
				continue
			}
			name = f[0]
		}
		seen[f[0]] = true
		list = append(list, btDevice{Mac: f[0], Name: name})
	}
	return list
}

// btAdapter tells whether the computer has Bluetooth, and whether it is on.
func btAdapter() (present, powered bool) {
	if m, _ := filepath.Glob("/sys/class/bluetooth/hci*"); len(m) == 0 {
		return false, false
	}
	out, err := runFor(5*time.Second, "bluetoothctl", "show")
	out = cleanBT(out)
	if err != nil || !strings.Contains(out, "Controller ") {
		return false, false
	}
	return true, strings.Contains(out, "Powered: yes")
}

// btList is the paired devices, connected first.
func btList() reply {
	present, powered := btAdapter()
	if !present {
		return reply{"ok": true, "present": false}
	}
	res := reply{"ok": true, "present": true, "powered": powered}
	out, err := runFor(5*time.Second, "bluetoothctl", "devices", "Paired")
	if err != nil {
		return fail(err, cleanBT(out))
	}
	paired := parseBTDevices(out, true)
	conn, _ := runFor(5*time.Second, "bluetoothctl", "devices", "Connected")
	on := map[string]bool{}
	for _, d := range parseBTDevices(conn, true) {
		on[d.Mac] = true
	}
	for i := range paired {
		paired[i].Paired = true
		paired[i].Connected = on[paired[i].Mac]
	}
	sort.SliceStable(paired, func(a, b int) bool { return paired[a].Connected && !paired[b].Connected })
	res["devices"] = paired
	return res
}

// btScan looks for devices for 10 s and lists the named ones not paired yet.
func btScan() reply {
	if present, _ := btAdapter(); !present {
		return reply{"ok": false, "error": "no Bluetooth"}
	}
	_, _ = runFor(15*time.Second, "bluetoothctl", "--timeout", "10", "scan", "on")
	out, err := runFor(5*time.Second, "bluetoothctl", "devices")
	if err != nil {
		return fail(err, cleanBT(out))
	}
	pout, _ := runFor(5*time.Second, "bluetoothctl", "devices", "Paired")
	paired := map[string]bool{}
	for _, d := range parseBTDevices(pout, true) {
		paired[d.Mac] = true
	}
	var found []btDevice
	for _, d := range parseBTDevices(out, false) {
		if !paired[d.Mac] {
			found = append(found, d)
		}
	}
	return reply{"ok": true, "devices": found}
}

func btPower(on bool) reply {
	state := "off"
	if on {
		state = "on"
	}
	out, err := runFor(10*time.Second, "bluetoothctl", "power", state)
	if err != nil {
		return fail(err, cleanBT(out))
	}
	return reply{"ok": true}
}

const btPairLog = "bluetooth-pair.log"

// btPair starts pairing in the background, because a keyboard needs a
// code typed on it first; btPairStatus follows the log. The agent shows
// codes and accepts the rest ("DisplayOnly").
func btPair(mac string) reply {
	if !macRe.MatchString(mac) {
		return reply{"ok": false, "error": "no device"}
	}
	f := logFile(btPairLog)
	if f == nil {
		return reply{"ok": false, "error": "no log"}
	}
	defer f.Close()
	// A device found by an earlier scan is forgotten after 30 s; look for
	// it again first when BlueZ no longer knows it.
	cmd := exec.Command("sh", "-c", `bluetoothctl info "$1" >/dev/null 2>&1 || bluetoothctl --timeout 8 scan on
exec bluetoothctl --agent DisplayOnly --timeout 60 pair "$1"`, "sh", mac)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	return reply{"ok": true}
}

var passkeyRe = regexp.MustCompile(`(?i)(?:passkey|pin code)[^0-9]{0,12}(\d{4,6})`)

// parsePair reads the pairing log: "done", "failed" or still waiting,
// with the code to type when there is one.
func parsePair(log string) reply {
	log = cleanBT(log)
	res := reply{"ok": true}
	if m := passkeyRe.FindAllStringSubmatch(log, -1); len(m) > 0 {
		res["passkey"] = m[len(m)-1][1]
	}
	switch {
	case strings.Contains(log, "Pairing successful"), strings.Contains(log, "AlreadyExists"):
		res["done"] = true
	case strings.Contains(log, "Failed to pair"), strings.Contains(log, "not available"),
		strings.Contains(log, "Timeout was reached"):
		res["done"] = true
		res["error"] = "pairing failed"
		for _, line := range strings.Split(log, "\n") {
			if strings.Contains(line, "Failed to pair") {
				res["error"] = strings.TrimSpace(line)
			}
		}
	}
	return res
}

func btPairStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	b, err := os.ReadFile(filepath.Join(home, "oros", "data", btPairLog))
	if err != nil {
		return reply{"ok": true}
	}
	return parsePair(string(b))
}

// btDo runs one device command: connect (trusting it first, so it comes
// back by itself next time), disconnect or forget.
func btDo(action, mac string) reply {
	if !macRe.MatchString(mac) {
		return reply{"ok": false, "error": "no device"}
	}
	steps := map[string][][]string{
		"connect":    {{"trust", mac}, {"connect", mac}},
		"disconnect": {{"disconnect", mac}},
		"forget":     {{"remove", mac}},
	}[action]
	if steps == nil {
		return reply{"ok": false, "error": "unknown command"}
	}
	for _, s := range steps {
		out, err := runFor(30*time.Second, "bluetoothctl", s...)
		out = cleanBT(out)
		if err != nil || strings.Contains(out, "Failed") || strings.Contains(out, "not available") {
			if err == nil {
				err = errFromOutput(out)
			}
			return fail(err, out)
		}
	}
	return reply{"ok": true}
}

type outputErr string

func (e outputErr) Error() string { return string(e) }

func errFromOutput(out string) error {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Failed") || strings.Contains(line, "not available") {
			return outputErr(strings.TrimSpace(line))
		}
	}
	return outputErr("failed")
}

type printer struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// parsePrinters reads "lpstat -e" (one destination per line, network
// printers included) and "lpstat -d".
func parsePrinters(dests, def string) []printer {
	d := ""
	if i := strings.Index(def, "destination:"); i >= 0 {
		d = strings.TrimSpace(def[i+len("destination:"):])
	}
	var list []printer
	for _, n := range strings.Split(dests, "\n") {
		n = strings.TrimSpace(n)
		if n == "" || strings.ContainsAny(n, " /") {
			continue
		}
		list = append(list, printer{Name: n, Default: n == d})
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Default && !list[b].Default })
	return list
}

var printerName = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

func printersList() reply {
	dests, err := runFor(10*time.Second, "lpstat", "-e")
	if err != nil && strings.TrimSpace(dests) != "" && !strings.Contains(dests, "No destinations") {
		return fail(err, dests)
	}
	def, _ := runFor(5*time.Second, "lpstat", "-d")
	return reply{"ok": true, "printers": parsePrinters(dests, def)}
}

// printTest prints CUPS's own test page, to check a printer works.
func printTest(name string) reply {
	if !printerName.MatchString(name) {
		return reply{"ok": false, "error": "no printer"}
	}
	out, err := runFor(30*time.Second, "lp", "-d", name, "-t", "orOS test page", "/usr/share/cups/data/testprint")
	if err != nil {
		return fail(err, out)
	}
	return reply{"ok": true}
}
