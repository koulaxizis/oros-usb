package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type request struct {
	Cmd      string `json:"cmd"`
	SSID     string `json:"ssid,omitempty"`
	Password string `json:"password,omitempty"`
	On       bool   `json:"on,omitempty"`
	Value    int    `json:"value,omitempty"`
	Disk     string `json:"disk,omitempty"`
	KeepData bool   `json:"keepData,omitempty"`
}

type reply map[string]any

// serveHost speaks Chromium's native messaging protocol: each message is a
// 32-bit length in native byte order followed by that much JSON.
func serveHost(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if n > 1<<20 {
			return fmt.Errorf("message too large: %d", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return err
		}
		var req request
		var res reply
		if err := json.Unmarshal(buf, &req); err != nil {
			res = reply{"ok": false, "error": "bad request"}
		} else {
			res = handle(req)
		}
		b, _ := json.Marshal(res)
		if err := binary.Write(out, binary.LittleEndian, uint32(len(b))); err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
	}
}

func handle(req request) reply {
	logf("cmd %s", req.Cmd)
	switch req.Cmd {
	case "status":
		return status()
	case "wifi.scan":
		out, err := run("nmcli", "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "yes")
		if err != nil {
			return fail(err, out)
		}
		return reply{"ok": true, "networks": parseWifiList(out)}
	case "wifi.connect":
		if req.SSID == "" {
			return reply{"ok": false, "error": "no network"}
		}
		args := []string{"--wait", "30", "device", "wifi", "connect", req.SSID}
		if req.Password != "" {
			args = append(args, "password", req.Password)
		}
		out, err := run("nmcli", args...)
		if err != nil {
			return fail(err, out)
		}
		return reply{"ok": true}
	case "wifi.radio":
		state := "off"
		if req.On {
			state = "on"
		}
		out, err := run("nmcli", "radio", "wifi", state)
		if err != nil {
			return fail(err, out)
		}
		return reply{"ok": true}
	case "volume.set":
		v := req.Value
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		out, err := run("wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", fmt.Sprintf("%.2f", float64(v)/100))
		if err != nil {
			return fail(err, out)
		}
		_, _ = run("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "0")
		return reply{"ok": true}
	case "volume.mute":
		m := "0"
		if req.On {
			m = "1"
		}
		out, err := run("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", m)
		if err != nil {
			return fail(err, out)
		}
		return reply{"ok": true}
	case "power.off", "power.reboot":
		action := "poweroff"
		if req.Cmd == "power.reboot" {
			action = "reboot"
		}
		out, err := run("systemctl", action)
		if err != nil {
			return fail(err, out)
		}
		return reply{"ok": true}
	case "web.open":
		return openWeb()
	case "install.disks":
		disks, err := installTargets()
		if err != nil {
			return fail(err, "")
		}
		return reply{"ok": true, "disks": disks}
	case "install.start":
		return startInstall(req)
	case "install.status":
		b, err := os.ReadFile(progressFile)
		if err != nil {
			return reply{"ok": true, "running": false}
		}
		var p progress
		_ = json.Unmarshal(b, &p)
		return reply{"ok": true, "running": !p.Done, "progress": p}
	}
	return reply{"ok": false, "error": "unknown command"}
}

func fail(err error, out string) reply {
	msg := strings.TrimSpace(out)
	if msg == "" {
		msg = err.Error()
	}
	if i := strings.LastIndex(msg, "Error: "); i >= 0 {
		msg = msg[i+7:]
	}
	return reply{"ok": false, "error": msg}
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func status() reply {
	res := reply{"ok": true}
	if out, err := run("nmcli", "-t", "-f", "WIFI", "radio"); err == nil {
		res["wifiOn"] = strings.TrimSpace(out) == "enabled"
	}
	if out, err := run("nmcli", "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "no"); err == nil {
		for _, n := range parseWifiList(out) {
			if n.InUse {
				res["wifi"] = n
			}
		}
	}
	if out, err := run("nmcli", "-t", "-f", "TYPE,STATE", "device"); err == nil {
		res["wired"] = strings.Contains(out, "ethernet:connected")
	}
	if out, err := run("wpctl", "get-volume", "@DEFAULT_AUDIO_SINK@"); err == nil {
		if v, muted, ok := parseVolume(out); ok {
			res["volume"] = v
			res["muted"] = muted
		}
	}
	if b := battery(); b != nil {
		res["battery"] = b
	}
	res["canInstall"] = liveMedium() != ""
	return res
}

type network struct {
	SSID   string `json:"ssid"`
	Signal int    `json:"signal"`
	Secure bool   `json:"secure"`
	InUse  bool   `json:"inUse"`
}

// splitTerse splits one line of "nmcli -t" output, where ':' inside a
// value is written as "\:".
func splitTerse(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++
			cur.WriteByte(line[i])
			continue
		}
		if c == ':' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	return append(out, cur.String())
}

// parseWifiList keeps one entry per network name, the strongest, and drops
// hidden networks.
func parseWifiList(out string) []network {
	best := map[string]int{}
	var list []network
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) < 4 || f[1] == "" {
			continue
		}
		sig, _ := strconv.Atoi(f[2])
		n := network{SSID: f[1], Signal: sig, Secure: f[3] != "" && f[3] != "--", InUse: f[0] == "*"}
		if i, ok := best[n.SSID]; ok {
			if n.InUse || (n.Signal > list[i].Signal && !list[i].InUse) {
				list[i] = n
			}
			continue
		}
		best[n.SSID] = len(list)
		list = append(list, n)
	}
	return list
}

var volRe = regexp.MustCompile(`Volume:\s*([0-9.]+)(\s*\[MUTED\])?`)

func parseVolume(out string) (int, bool, bool) {
	m := volRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false, false
	}
	return int(f*100 + 0.5), m[2] != "", true
}

func battery() reply {
	paths, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	for _, p := range paths {
		c, err := os.ReadFile(filepath.Join(p, "capacity"))
		if err != nil {
			continue
		}
		pct, _ := strconv.Atoi(strings.TrimSpace(string(c)))
		st, _ := os.ReadFile(filepath.Join(p, "status"))
		return reply{"percent": pct, "charging": strings.TrimSpace(string(st)) == "Charging"}
	}
	return nil
}

func startInstall(req request) reply {
	if req.Disk == "" || !regexp.MustCompile(`^[a-z0-9]+$`).MatchString(req.Disk) {
		return reply{"ok": false, "error": "no disk"}
	}
	args := []string{"-n", "/opt/oros/oros-system", "install", "--disk", "/dev/" + req.Disk, "--yes"}
	if req.KeepData {
		home, _ := os.UserHomeDir()
		args = append(args, "--home", home)
	}
	cmd := exec.Command("sudo", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	// The installer outlives this short-lived host process.
	go func() { _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true}
}

// webBrowser and webArgs open a normal Chromium window for the web, with
// tabs and an address bar, in its own profile next to orOS's. cage shows
// it over orOS; closing it brings orOS back.
var webBrowser = "/usr/bin/chromium"

func webArgs(home string) []string {
	return []string{
		"--ozone-platform=wayland",
		"--user-data-dir=" + filepath.Join(home, "oros", "data", "web"),
		"--no-first-run",
		"--disable-features=Translate",
		"--new-window",
		"https://duckduckgo.com/",
	}
}

func openWeb() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	cmd := exec.Command(webBrowser, webArgs(home)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	// The browser outlives this short-lived host process.
	go func() { _ = cmd.Wait() }()
	return reply{"ok": true}
}

// logf keeps a short log next to orOS's data; the boot test reads it to
// see that the extension reached this host.
func logf(format string, a ...any) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, "oros", "data")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	p := filepath.Join(dir, "oros-system.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 256<<10 {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, a...))
}
