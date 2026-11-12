package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	// Passphrase encrypts the installed disk's data (install.start).
	Passphrase string `json:"passphrase,omitempty"`
	Zone       string `json:"zone,omitempty"`
	Mac        string `json:"mac,omitempty"`
	Printer    string `json:"printer,omitempty"`
	// Web is orOS's own part of a problem report (Settings › System ›
	// Report a problem, already shown to the person).
	Web string `json:"web,omitempty"`
	// Model is a local AI model (ai.install, ai.forget; ai.go).
	Model string `json:"model,omitempty"`
	// Kid is the page's id for its screen lock record (lockbio.go); Text
	// the line a prompt shows (unused here: fprintd has no window).
	Kid  string `json:"kid,omitempty"`
	Text string `json:"text,omitempty"`
}

type reply map[string]any

// serveHost speaks Chromium's native messaging protocol: each message is a
// 32-bit length in native byte order followed by that much JSON.
func serveHost(in io.Reader, out io.Writer) error {
	if b, err := os.ReadFile("/proc/cmdline"); err == nil {
		logf("system %s slot=%q", currentVersion(), runningSlot(string(b)))
	}
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
	// The watchdog's ping comes every few seconds; logging it would bury the rest.
	if req.Cmd != "ping" {
		logf("cmd %s", req.Cmd)
	}
	switch req.Cmd {
	case "ping":
		return ping()
	case "bridge.code":
		return reply{"ok": true, "code": bridgeCode()}
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
	case "update.check":
		return checkUpdate()
	case "update.start":
		if !ownerOnly() {
			return reply{"ok": false, "error": "owner only"}
		}
		return startRoot("update", "--yes")
	case "update.status":
		return readProgress(updateProgressFile)
	case "time.get":
		return timeGet()
	case "time.zone":
		return timeZone(req.Zone)
	case "brightness.set":
		return setBrightness(req.Value)
	case "bt.list":
		return btList()
	case "bt.scan":
		return btScan()
	case "bt.power":
		return btPower(req.On)
	case "bt.pair":
		return btPair(req.Mac)
	case "bt.pairStatus":
		return btPairStatus()
	case "bt.connect", "bt.disconnect", "bt.forget":
		return btDo(strings.TrimPrefix(req.Cmd, "bt."), req.Mac)
	case "printers.list":
		return printersList()
	case "printers.test":
		return printTest(req.Printer)
	case "web.open":
		return openWeb(webHome())
	case "tor.open":
		return openTor()
	case "tor.status":
		return torStatus()
	case "tor.remove":
		return removeTor()
	case "ai.status":
		return aiStatus()
	case "ai.start":
		return aiStart()
	case "ai.install", "ai.forget", "ai.remove", "ai.gpu":
		// The models are shared by everyone on the stick and take gigabytes:
		// only the owner adds or removes them.
		if !ownerOnly() {
			return reply{"ok": false, "error": "owner only"}
		}
		switch req.Cmd {
		case "ai.install":
			return aiInstall(req.Model)
		case "ai.forget":
			return aiForget(req.Model)
		case "ai.gpu":
			return aiSetGPU(req.On)
		}
		return aiRemove()
	case "bio.state", "bio.seal", "bio.open", "bio.clear":
		return bioHost(req)
	case "report":
		return report(req.Web)
	case "disks.list":
		return disksList()
	case "disks.open":
		return diskOpen(req.Disk)
	case "disks.mount":
		return diskMount(req.Disk)
	case "disks.close":
		return diskClose(req.Disk)
	case "install.disks":
		disks, err := installTargets()
		if err != nil {
			return fail(err, "")
		}
		return reply{"ok": true, "disks": disks}
	case "install.start":
		if !ownerOnly() {
			return reply{"ok": false, "error": "owner only"}
		}
		return startInstall(req)
	case "install.status":
		return readProgress(progressFile)
	}
	return reply{"ok": false, "error": "unknown command"}
}

// ping tells oros-session's watchdog that the orOS window has painted and
// its page still runs. OROS_ALIVE (set by oros-session, passed on by
// Chromium) names a file in RAM, so the stick is not written every few
// seconds.
func ping() reply {
	res := reply{"ok": true}
	if c := bridgeCode(); c != "" {
		res["bridgeCode"] = c
	}
	p := os.Getenv("OROS_ALIVE")
	if p == "" {
		return res
	}
	if err := os.WriteFile(p, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)+"\n"), 0o600); err != nil {
		return fail(err, "")
	}
	return res
}

// bridgeCode is orOS Bridge's open pairing code, or "". oros-session starts
// the bridge with --code-file $OROS_BRIDGE_CODE (in RAM); the file holds six
// digits and a newline while Studio waits for the code and is gone otherwise.
// The System extension shows it over orOS, since the bridge has no window here.
func bridgeCode() string {
	p := os.Getenv("OROS_BRIDGE_CODE")
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	c := strings.TrimSpace(string(b))
	if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
		return ""
	}
	return c
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
	if v, ok := brightness(); ok {
		res["brightness"] = v
	}
	res["zone"] = currentZone()
	res["canInstall"] = liveMedium() != "" && !installedSystem()
	// Another user than the owner is signed in: the extension hides the
	// system update and the install (and handle refuses them).
	res["notOwner"] = !ownerOnly()
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
	if req.Passphrase != "" {
		if err := checkPassphrase(req.Passphrase); err != nil {
			return reply{"ok": false, "error": err.Error()}
		}
		args = append(args, "--encrypt")
	}
	cmd := exec.Command("sudo", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The passphrase goes through a pipe, never on the command line. It is
	// written before the start and fits the pipe's buffer, so the installer
	// gets it even after this short-lived host process has gone.
	if req.Passphrase != "" {
		r, w, err := os.Pipe()
		if err != nil {
			return fail(err, "")
		}
		_, werr := w.Write([]byte(req.Passphrase + "\n"))
		w.Close()
		if werr != nil {
			r.Close()
			return fail(werr, "")
		}
		cmd.Stdin = r
		defer r.Close()
	}
	// Its steps go to a log file, not a pipe: the installer outlives this
	// short-lived host process, and writing to a closed pipe would stop it.
	if f := logFile("install.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true}
}

func readProgress(file string) reply {
	b, err := os.ReadFile(file)
	if err != nil {
		return reply{"ok": true, "running": false}
	}
	var p progress
	_ = json.Unmarshal(b, &p)
	return reply{"ok": true, "running": !p.Done, "progress": p}
}

// checkUpdate compares the running system with the latest release.
func checkUpdate() reply {
	cur := currentVersion()
	r := reply{"ok": true, "current": cur, "canUpdate": persistenceTarget() != ""}
	m, err := fetchManifest(updateURL)
	if errors.Is(err, errNoRelease) {
		r["available"] = false
		return r
	}
	if err != nil {
		return fail(err, "")
	}
	r["latest"] = m.Version
	r["size"] = m.size()
	r["available"] = m.Version != cur
	return r
}

// startRoot runs oros-system as root through the narrow sudo rule, in
// the background; it outlives this short-lived host process.
func startRoot(args ...string) reply {
	cmd := exec.Command("sudo", append([]string{"-n", "/opt/oros/oros-system"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile(args[0] + ".log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true}
}

func webUserDir(home string) string {
	dir, _ := signedInUser(home)
	return dir
}

// ownerOnly: with users on the stick, the system update and the install
// to a disk are the owner's (they change the computer for everybody).
func ownerOnly() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, owner := signedInUser(home)
	return owner
}

// The Internet button's first page: orOS's Start page (startpage/,
// orOS 5.33 and later: favourites from Bookmarks, search, the Tor button)
// when the orOS this stick serves has it, otherwise DuckDuckGo.
var (
	webStart    = "http://localhost:47800/startpage/"
	webFallback = "https://duckduckgo.com/"
)

func webHome() string {
	c := http.Client{Timeout: 800 * time.Millisecond}
	r, err := c.Get(webStart)
	if err != nil {
		return webFallback
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return webFallback
	}
	return webStart
}

// webBrowser and webArgs open a normal Chromium window for the web, with
// tabs and an address bar. cage shows it over orOS; closing it brings orOS
// back. It opens in orOS's own browser profile (the launcher's
// <user folder>/profile), so it joins the browser that shows orOS: the
// orOS add-on there fills and saves Passwords logins on the web pages and
// reaches the orOS window to do it. Sites therefore share the browser's
// storage with the orOS window, as in any browser with orOS in a tab.
var webBrowser = "/usr/bin/chromium"

// webExtensions are the extensions the orOS window starts with
// (oros-session ARGS): the System extension, the orOS add-on with its
// address preset to this stick (scripts/addon.sh) and uBlock Origin Lite
// (scripts/ubol.sh), which runs from oros-session's writable copy in
// ~/oros/ubol (Chromium writes its indexed rules into the folder). The
// browser loads them when it starts; the Internet window lists them too,
// for the case it is the first window on the profile. Missing ones (a
// build without them) are left out.
var webExtensions = func(home string) []string {
	return []string{"/opt/oros/system-ext", "/opt/oros/addon", filepath.Join(home, "oros", "ubol")}
}

// webProfile is the signed-in user's orOS browser profile (users.go and
// the launcher's browserCandidates).
func webProfile(home string) string {
	return filepath.Join(webUserDir(home), "profile")
}

func webArgs(home, url string) []string {
	args := []string{
		"--ozone-platform=wayland",
		"--user-data-dir=" + webProfile(home),
		"--no-first-run",
		"--disable-features=Translate",
		"--enable-logging=stderr",
		"--new-window",
	}
	// oros-session's watchdog turned the GPU off for the orOS window after
	// it stayed blank; the Internet window would stay blank the same way.
	if os.Getenv("OROS_GPU_OFF") == "1" {
		args = append(args, "--disable-gpu")
	}
	var ext []string
	for _, d := range webExtensions(home) {
		if _, err := os.Stat(filepath.Join(d, "manifest.json")); err == nil {
			ext = append(ext, d)
		}
	}
	if len(ext) > 0 {
		args = append(args, "--load-extension="+strings.Join(ext, ","))
	}
	return append(args, url)
}

func openWeb(url string) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	data := filepath.Join(home, "oros", "data")
	if err := os.MkdirAll(webProfile(home), 0o700); err != nil {
		return fail(err, "")
	}
	cmd := exec.Command(webBrowser, webArgs(home, url)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The browser's own messages, to find out why a window did not open.
	if f, err := os.Create(filepath.Join(data, "web.log")); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	// The browser outlives this short-lived host process.
	go func() { _ = cmd.Wait() }()
	return reply{"ok": true}
}

// logFile opens a fresh log in ~/oros/data for a background job.
func logFile(name string) *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.Create(filepath.Join(home, "oros", "data", name))
	if err != nil {
		return nil
	}
	fmt.Fprintf(f, "== %s\n", time.Now().Format(time.RFC3339))
	return f
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
