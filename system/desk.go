package main

// The desktop parts of the orOS USB session that GNOME has and the kiosk
// lacked (orOS gap list, 2026-11-15): sleep, the screen going off when
// nobody uses the computer, keyboard layouts, the Orca screen reader with
// local espeak-ng voices, and PrtSc screenshots into Files.
//
// The choices live in ~/oros/data/desk.json, the oros user's own file
// (no root needed; the installer copies /home with it). Every field is
// checked when it is read, so a broken or hand-edited file falls back to
// the defaults field by field and never reaches a command line unchecked.
//
//   - oros-session reads the keyboard with "oros-system keyboard --layouts"
//     and "--options" before it starts labwc (labwc takes the layout from
//     XKB_DEFAULT_* only when it starts, so a new layout needs a restart
//     of the orOS session, which the panel offers).
//   - oros-kiosk (inside labwc) runs "oros-system idle": swayidle with the
//     chosen timeouts, started again whenever desk.json changes.
//   - oros-kiosk runs "oros-system reader --session", which starts Orca
//     when the screen reader is on. Chromium starts with
//     --force-renderer-accessibility only then (oros-session asks
//     "oros-system reader --enabled"), so turning it on restarts the
//     session once.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type deskSettings struct {
	// ScreenOff and Sleep are minutes without use; 0 is never.
	ScreenOff int `json:"screenOff"`
	Sleep     int `json:"sleep"`
	// Layouts are XKB layouts, the first one active at the start.
	Layouts []string `json:"layouts"`
	// Switch is the key combination that moves to the next layout.
	Switch string `json:"switch"`
	Reader bool   `json:"reader"`
	// ReaderLang is orOS's language when the reader was turned on: Orca
	// and its espeak-ng voice start in it.
	ReaderLang string `json:"readerLang,omitempty"`
	// Display is how several screens are used (display.go).
	Display displaySettings `json:"display"`
}

var (
	screenOffChoices = []int{0, 2, 5, 10, 15, 30}
	sleepChoices     = []int{0, 15, 30, 60, 120}
	// The layouts the panel offers (XKB names); orOS's own languages first.
	layoutChoices = []string{"us", "gr", "de", "gb", "fr", "es", "it", "pt", "nl", "be", "ch", "at",
		"se", "no", "dk", "fi", "pl", "cz", "sk", "hu", "ro", "bg", "rs", "hr", "si", "al", "tr", "ru", "ua", "il", "ara"}
	switchOptions = map[string]string{
		"alt_shift":  "grp:alt_shift_toggle",
		"win_space":  "grp:win_space_toggle",
		"ctrl_shift": "grp:ctrl_shift_toggle",
		"caps":       "grp:caps_toggle",
	}
	readerLangs = map[string]string{"en": "en_US.UTF-8", "el": "el_GR.UTF-8", "de": "de_DE.UTF-8"}
	maxLayouts  = 4
)

func defaultDesk() deskSettings {
	return deskSettings{ScreenOff: 10, Sleep: 0, Layouts: []string{"us", "gr"}, Switch: "alt_shift", Display: displaySettings{Mode: "extend"}}
}

func deskFile(home string) string { return filepath.Join(home, "oros", "data", "desk.json") }

func intIn(v int, list []int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func strIn(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// cleanLayouts keeps known layouts, each once, at most maxLayouts; nil
// when none is left.
func cleanLayouts(in []string) []string {
	var out []string
	for _, l := range in {
		if strIn(l, layoutChoices) && !strIn(l, out) && len(out) < maxLayouts {
			out = append(out, l)
		}
	}
	return out
}

// sanitize replaces every unknown value with its default.
func (d deskSettings) sanitize() deskSettings {
	def := defaultDesk()
	if !intIn(d.ScreenOff, screenOffChoices) {
		d.ScreenOff = def.ScreenOff
	}
	if !intIn(d.Sleep, sleepChoices) {
		d.Sleep = def.Sleep
	}
	if d.Layouts = cleanLayouts(d.Layouts); d.Layouts == nil {
		d.Layouts = def.Layouts
	}
	if _, ok := switchOptions[d.Switch]; !ok {
		d.Switch = def.Switch
	}
	if _, ok := readerLangs[d.ReaderLang]; !ok {
		d.ReaderLang = ""
	}
	d.Display = d.Display.sanitize()
	return d
}

func readDesk(home string) deskSettings {
	b, err := os.ReadFile(deskFile(home))
	if err != nil {
		return defaultDesk()
	}
	d := defaultDesk()
	if json.Unmarshal(b, &d) != nil {
		return defaultDesk()
	}
	return d.sanitize()
}

func writeDesk(home string, d deskSettings) error {
	b, _ := json.MarshalIndent(d.sanitize(), "", "  ")
	p := deskFile(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// xkbLayouts and xkbOptions are what oros-session hands labwc.
func (d deskSettings) xkbLayouts() string { return strings.Join(d.Layouts, ",") }

func (d deskSettings) xkbOptions() string {
	if len(d.Layouts) < 2 {
		return ""
	}
	return switchOptions[d.Switch]
}

// ---------- host commands (the System panel) ----------

func deskGet() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	d := readDesk(home)
	_, orca := exec.LookPath("orca")
	return reply{"ok": true, "desk": d,
		"screenOffChoices": screenOffChoices, "sleepChoices": sleepChoices,
		"layoutChoices": layoutChoices, "switchChoices": []string{"alt_shift", "win_space", "ctrl_shift", "caps"},
		// The layouts labwc runs with now (oros-session's environment,
		// passed on by Chromium); different from the saved ones until the
		// session starts again.
		"activeLayouts":   os.Getenv("XKB_DEFAULT_LAYOUT"),
		"readerAvailable": orca == nil,
		"readerRunning":   readerRunning(),
		// Chromium gives the screen reader the page only when it started
		// with accessibility on.
		"readerReady": hasArg("--force-renderer-accessibility"),
	}
}

// hasArg: did oros-session start this browser with the flag? The host's
// parent is the Chromium that shows orOS.
func hasArg(flag string) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", os.Getppid()))
	if err != nil {
		return false
	}
	for _, a := range strings.Split(string(b), "\x00") {
		if a == flag {
			return true
		}
	}
	return false
}

// deskSet saves the panel's choices. The screen and sleep times reach
// swayidle by themselves ("oros-system idle" watches the file); turning
// the reader off stops Orca now; the keyboard and turning the reader on
// wait for the session's restart, which the panel asks for.
func deskSet(raw json.RawMessage) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	d := readDesk(home)
	if len(raw) == 0 || json.Unmarshal(raw, &d) != nil {
		return reply{"ok": false, "error": "bad settings"}
	}
	d = d.sanitize()
	if err := writeDesk(home, d); err != nil {
		return fail(err, "")
	}
	if !d.Reader {
		readerStop()
	}
	return reply{"ok": true, "desk": d}
}

// sessionRestart ends labwc; oros-session starts orOS again at once (the
// way it does when the orOS window closes). orOS keeps its data in the
// browser as it goes, so nothing is lost.
func sessionRestart() reply {
	out, err := run("pkill", "-TERM", "-u", strconv.Itoa(os.Getuid()), "-x", "labwc")
	if err != nil {
		return fail(err, out)
	}
	return reply{"ok": true}
}

func suspend() reply {
	out, err := run("systemctl", "suspend")
	if err != nil {
		return fail(err, out)
	}
	return reply{"ok": true}
}

// ---------- keyboard (oros-session) ----------

// keyboardMain: "oros-system keyboard --layouts" prints "us,gr",
// "--options" prints "grp:alt_shift_toggle" (or nothing).
func keyboardMain(args []string) int {
	home, _ := os.UserHomeDir()
	d := readDesk(home)
	if len(args) == 1 && args[0] == "--layouts" {
		fmt.Println(d.xkbLayouts())
		return 0
	}
	if len(args) == 1 && args[0] == "--options" {
		fmt.Println(d.xkbOptions())
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: oros-system keyboard --layouts | --options")
	return 2
}

// ---------- the screen going off and sleep (oros-kiosk, inside labwc) ----------

var swayidleBin = "swayidle"

// idleArgs are swayidle's arguments for the settings; nil when nothing
// is to happen. The screen comes back on with any key or move, and after
// a sleep. Chromium asks labwc not to go idle while a video plays
// (idle-inhibit), which swayidle respects.
func idleArgs(self string, d deskSettings) []string {
	var a []string
	if d.ScreenOff > 0 {
		a = append(a, "timeout", strconv.Itoa(d.ScreenOff*60), self+" screen --off",
			"resume", self+" screen --on")
	}
	if d.Sleep > 0 {
		a = append(a, "timeout", strconv.Itoa(d.Sleep*60), "systemctl suspend")
	}
	if a == nil {
		return nil
	}
	if d.ScreenOff > 0 {
		a = append(a, "after-resume", self+" screen --on")
	}
	return a
}

// idleMain is "oros-system idle": swayidle with the saved times, started
// again when desk.json changes, until the session that started it ends
// (its parent changes) or Wayland is gone. It also applies the screens'
// layout (display.go) when the session starts and whenever a screen is
// plugged in or out.
func idleMain() int {
	home, _ := os.UserHomeDir()
	self, err := os.Executable()
	if err != nil {
		self = "/opt/oros/oros-system"
	}
	parent := os.Getppid()
	var child *exec.Cmd
	var done chan struct{}
	stop := func() {
		if child != nil {
			_ = child.Process.Signal(syscall.SIGTERM)
			<-done
		}
		child = nil
	}
	defer stop()
	var seen time.Time
	failures := 0
	screens := ""
	// games and videos that ask the screen to stay on (games.go)
	holds := serveScreenSaver()
	heldBy := ""
	for {
		if k := outputsKey(); k != "" && k != screens {
			screens = k
			if err := applyDisplay(readDesk(home).Display); err != nil {
				logf("display: %v", err)
			}
		}
		if os.Getppid() != parent || os.Getenv("WAYLAND_DISPLAY") == "" {
			return 0
		}
		// orOS's Keep awake: no swayidle while it holds; it starts again
		// with the saved times once the hold ends.
		if awakeHeld(time.Now()) {
			if child != nil {
				stop()
				logf("idle: orOS keeps the screen awake")
			}
			time.Sleep(5 * time.Second)
			continue
		}
		if holds != nil {
			if on, app := holds.held(); on {
				if child != nil || app != heldBy {
					stop()
					logf("idle: %q keeps the screen awake", app)
				}
				heldBy = app
				time.Sleep(5 * time.Second)
				continue
			}
			heldBy = ""
		}
		var mt time.Time
		if st, err := os.Stat(deskFile(home)); err == nil {
			mt = st.ModTime()
		}
		exited := false
		if child != nil {
			select {
			case <-done:
				exited = true
				child = nil
			default:
			}
		}
		if child == nil || !mt.Equal(seen) || exited {
			stop()
			seen = mt
			if exited {
				failures++
				if failures > 5 {
					logf("idle: swayidle keeps stopping; the screen stays on")
					return 1
				}
			}
			if a := idleArgs(self, readDesk(home)); a != nil {
				c := exec.Command(swayidleBin, a...)
				if err := c.Start(); err != nil {
					logf("idle: %v", err)
					return 1
				}
				child, done = c, make(chan struct{})
				go func(c *exec.Cmd, d chan struct{}) { _ = c.Wait(); close(d) }(c, done)
			}
		}
		time.Sleep(5 * time.Second)
	}
}

// screenMain is "oros-system screen --off|--on" (swayidle). wlopm turns
// the outputs off where the compositor has wlr-output-power-management.
// Where it does not (Debian 13's cage did not; the CI boot test records
// what labwc does), a laptop's
// backlight goes to 0 and comes back to where it was, and the System
// extension covers orOS with black (blankFile, read through its ping)
// until the next key or move, which spares a desktop monitor's picture
// even though its own light stays on.
func screenMain(args []string) int {
	if len(args) != 1 || (args[0] != "--off" && args[0] != "--on") {
		fmt.Fprintln(os.Stderr, "usage: oros-system screen --off | --on")
		return 2
	}
	saved := runtimeFile("oros-backlight")
	if args[0] == "--off" {
		if _, err := run("wlopm", "--off", "*"); err == nil {
			return 0
		}
		_ = os.WriteFile(blankFile(), []byte("1\n"), 0o600)
		if out, err := run("brightnessctl", "-m", "-c", "backlight", "get"); err == nil {
			if _, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
				_ = os.WriteFile(saved, []byte(strings.TrimSpace(out)+"\n"), 0o600)
				_, _ = run("brightnessctl", "-q", "-c", "backlight", "set", "0")
			}
		}
		return 0
	}
	_, _ = run("wlopm", "--on", "*")
	_ = os.Remove(blankFile())
	if b, err := os.ReadFile(saved); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && v > 0 {
			_, _ = run("brightnessctl", "-q", "-c", "backlight", "set", strconv.Itoa(v))
		}
		_ = os.Remove(saved)
	}
	return 0
}

func blankFile() string { return runtimeFile("oros-blank") }

// ---------- Keep awake (orOS Quick settings) ----------

// awakeFile is touched by every ping while orOS's Keep awake is on and
// removed by the first ping without it. It lives in RAM, so a new boot
// never starts held.
func awakeFile() string { return runtimeFile("oros-awake-" + strconv.Itoa(os.Getuid())) }

// awakeFresh: pings come every 10 s; a hold not renewed for 45 s (orOS
// closed or stuck) lets the screen go off and the computer sleep again.
const awakeFresh = 45 * time.Second

func awakeMark(on bool) {
	if !on {
		_ = os.Remove(awakeFile())
		return
	}
	_ = os.WriteFile(awakeFile(), []byte("1\n"), 0o600)
	now := time.Now()
	_ = os.Chtimes(awakeFile(), now, now)
}

// awakeHeld: orOS asked to keep the screen on, recently enough.
func awakeHeld(now time.Time) bool {
	st, err := os.Stat(awakeFile())
	return err == nil && now.Sub(st.ModTime()) < awakeFresh
}

func runtimeFile(name string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, name)
}

// ---------- screen reader ----------

func readerRunning() bool {
	_, err := run("pgrep", "-u", strconv.Itoa(os.Getuid()), "-x", "orca")
	return err == nil
}

func readerStop() {
	_, _ = run("pkill", "-u", strconv.Itoa(os.Getuid()), "-x", "orca")
}

// readerMain: "--enabled" exits 0 when the reader is on (oros-session adds
// --force-renderer-accessibility then); "--session" runs Orca for this
// session when it is on (oros-kiosk, inside labwc), in orOS's language
// so espeak-ng speaks it.
func readerMain(args []string) int {
	home, _ := os.UserHomeDir()
	d := readDesk(home)
	if len(args) == 1 && args[0] == "--enabled" {
		if d.Reader {
			return 0
		}
		return 1
	}
	if len(args) == 1 && args[0] == "--session" {
		if !d.Reader {
			return 0
		}
		c := exec.Command("orca", "--replace")
		c.Env = readerEnv(os.Environ(), d.ReaderLang)
		if f := logFile("orca.log"); f != nil {
			c.Stdout, c.Stderr = f, f
			defer f.Close()
		}
		if err := c.Run(); err != nil {
			logf("reader: %v", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: oros-system reader --enabled | --session")
	return 2
}

// readerEnv sets the locale for Orca alone (the session stays C.UTF-8),
// when the image has it (locale-gen in the image hook).
func readerEnv(env []string, lang string) []string {
	loc, ok := readerLangs[lang]
	if !ok || !localeExists(loc) {
		return env
	}
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, "LANG=") && !strings.HasPrefix(e, "LANGUAGE=") && !strings.HasPrefix(e, "LC_ALL=") {
			out = append(out, e)
		}
	}
	return append(out, "LANG="+loc, "LANGUAGE="+strings.SplitN(loc, ".", 2)[0])
}

var localeExists = func(loc string) bool {
	out, err := run("locale", "-a")
	if err != nil {
		return false
	}
	want := strings.ToLower(strings.ReplaceAll(loc, "UTF-8", "utf8"))
	for _, l := range strings.Fields(out) {
		if strings.ToLower(l) == want {
			return true
		}
	}
	return false
}

// ---------- PrtSc ----------

var grimBin = "grim"

// filesHome is the signed-in user's home, the folder behind Files' "This
// computer › Home" (the orOS Portable launcher's userHome): the first
// user keeps the computer's home, the others have ~/oros/homes/<id>.
func filesHome(home string) string {
	dir, _ := signedInUser(home)
	data := filepath.Join(home, "oros", "data")
	if dir == data {
		return home
	}
	return filepath.Join(home, "oros", "homes", filepath.Base(dir))
}

// screenshot saves the whole screen (grim, through labwc's screencopy) as
// Pictures/Screenshots/Screenshot <date> <time>.png and answers with the
// path Files opens ("/home/Pictures/Screenshots").
func screenshot() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := filepath.Join(filesHome(home), "Pictures", "Screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err, "")
	}
	base := "Screenshot " + time.Now().Format("2006-01-02 15-04-05")
	name := base + ".png"
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%s (%d).png", base, i)
	}
	tmp := filepath.Join(dir, ".shot-"+strconv.Itoa(os.Getpid())+".png")
	if out, err := run(grimBin, "-t", "png", tmp); err != nil {
		_ = os.Remove(tmp)
		return fail(err, out)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return fail(err, "")
	}
	return reply{"ok": true, "file": name, "folder": "/home/Pictures/Screenshots"}
}

// selftestDeskMain is "oros-system selftest-desk", run by oros-kiosk in
// boot tests only (/home/.oros-selftest): inside labwc it takes a
// screenshot, turns the screen off and on, and checks the keyboard
// layouts, swayidle, the espeak-ng voices and Orca. What happened goes
// to ~/.oros-selftest-desk for test-boot.sh; the lines marked "info"
// never fail the test (they say which way the screen went off).
func selftestDeskMain() int {
	home, _ := os.UserHomeDir()
	var b strings.Builder
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(&b, "FAIL %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(&b, "ok %s\n", name)
	}
	info := func(name, s string) { fmt.Fprintf(&b, "info %s: %s\n", name, strings.TrimSpace(s)) }
	time.Sleep(40 * time.Second) // let orOS paint first

	d := readDesk(home)
	if got := os.Getenv("XKB_DEFAULT_LAYOUT"); got != d.xkbLayouts() {
		check("keyboard", fmt.Errorf("labwc runs with %q, saved %q", got, d.xkbLayouts()))
	} else {
		check("keyboard "+got+" "+os.Getenv("XKB_DEFAULT_OPTIONS"), nil)
	}

	r := screenshot()
	if ok, _ := r["ok"].(bool); !ok {
		check("screenshot", fmt.Errorf("%v", r["error"]))
	} else {
		p := filepath.Join(filesHome(home), "Pictures", "Screenshots", fmt.Sprint(r["file"]))
		f, err := os.ReadFile(p)
		if err == nil && (len(f) < 1000 || string(f[1:4]) != "PNG") {
			err = fmt.Errorf("not a PNG (%d bytes)", len(f))
		}
		check("screenshot "+fmt.Sprint(r["file"]), err)
		_ = os.Remove(p)
	}

	out, err := run("wlopm")
	info("wlopm", fmt.Sprintf("%v %s", err, out))
	_, offErr := run("wlopm", "--off", "*")
	info("screen off through labwc", fmt.Sprint(offErr == nil))
	_, _ = run("wlopm", "--on", "*")

	// Night Light's real gamma (night.go): info only, orOS falls back to
	// its own filter where labwc has no wlr-gamma-control.
	nr := nightSet(4000)
	info("night light gamma (wlsunset on labwc)", fmt.Sprint(nr["ok"] == true, " ", nr["error"]))
	nightSet(0)

	if l, err := listOutputs(); err != nil || len(l) == 0 {
		check("screens (wlr-randr)", fmt.Errorf("%v, %d screens", err, len(l)))
	} else {
		check("screens (wlr-randr) "+outputsKey(), applyDisplay(readDesk(home).Display))
	}

	_, err = run("pgrep", "-u", strconv.Itoa(os.Getuid()), "-x", "swayidle")
	check("swayidle", err)

	for _, v := range []string{"en", "el", "de"} {
		wav, err := exec.Command("espeak-ng", "-v", v, "--stdout", "orOS").Output()
		if err == nil && len(wav) < 1000 {
			err = fmt.Errorf("%d bytes", len(wav))
		}
		check("espeak-ng "+v, err)
	}
	selftestGames(check, info)

	out, err = run("orca", "--version")
	check("orca "+strings.TrimSpace(out), err)
	out, _ = run("spd-say", "--list-output-modules")
	info("speech-dispatcher modules", strings.ReplaceAll(out, "\n", " "))
	out, _ = run("locale", "-a")
	info("locales", strings.ReplaceAll(out, "\n", " "))

	if failed {
		b.WriteString("FAIL\n")
	} else {
		b.WriteString("PASS\n")
	}
	_ = os.WriteFile(filepath.Join(home, ".oros-selftest-desk"), []byte(b.String()), 0o644)
	if failed {
		return 1
	}
	return 0
}
