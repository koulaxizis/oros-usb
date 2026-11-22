package main

// The software center (Settings › Software, wave G3, Chris 2026-11-22:
// «κατάστημα με επιλεγμένο λογισμικό που έχουμε δοκιμάσει», our own first).
// Only what the catalog that ships with the image lists can be installed:
//
//   - "flatpak": programs and game stores from Flathub, in the person's
//     home folder (flatpak --user, so they stay on the persistence
//     partition). The Flathub remote carries a filter that the build wrote
//     from the same catalog (/opt/oros/flathub.filter: every app denied
//     but the listed ones), so nothing else can come from it.
//   - "builtin": orOS's own buttons (local AI, Claude desktop, Tor
//     Browser, NVIDIA's driver); their own commands install them, the
//     center only shows them first.
//   - "addon": Chromium add-ons for the Internet window, one click: root
//     writes Chromium's ExtensionInstallForcelist policy with the chosen
//     ones (addons.go), so Chromium itself installs, checks and updates
//     them from the Chrome Web Store.
//   - "models": more open models for the local AI (ai.go offers them).
//
// Flatpak programs update by themselves once a day at the session start
// (soft --auto).

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	catalogFile  = "/opt/oros/catalog.json"
	flathubRepo  = "/opt/oros/flathub.flatpakrepo"
	flathubCheck = "/opt/oros/flathub.filter"
	flatpakBin   = "flatpak"
)

type catText map[string]string

type catItem struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Section string  `json:"section"`
	MB      int     `json:"mb,omitempty"`
	Name    catText `json:"name"`
	Summary catText `json:"summary"`
	Licence string  `json:"licence,omitempty"`
	Home    string  `json:"home,omitempty"`
}

type catModel struct {
	Name      string  `json:"name"`
	MB        int     `json:"mb"`
	MinMemGiB float64 `json:"minMemGiB"`
	Licence   string  `json:"licence"`
	By        string  `json:"by"`
	Summary   catText `json:"summary"`
}

type catalog struct {
	Version int        `json:"version"`
	Items   []catItem  `json:"items"`
	Models  []catModel `json:"models"`
}

var (
	flatpakIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+){2,}$`)
	addonIDRe   = regexp.MustCompile(`^[a-p]{32}$`)
	modelRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*:[a-z0-9][a-z0-9._-]*$`)
	builtins    = map[string]bool{"ai": true, "claude": true, "tor": true, "nvidia": true}
)

// parseCatalog reads the catalog and refuses it whole when one entry is
// not what its kind allows (the ids go to flatpak, to Chromium's policy
// and to Ollama).
func parseCatalog(b []byte) (catalog, error) {
	var c catalog
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, fmt.Errorf("catalog version %d", c.Version)
	}
	seen := map[string]bool{}
	for _, it := range c.Items {
		ok := false
		switch it.Kind {
		case "flatpak":
			ok = flatpakIDRe.MatchString(it.ID) && it.MB > 0
		case "addon":
			ok = addonIDRe.MatchString(it.ID)
		case "builtin":
			ok = builtins[it.ID]
		}
		if !ok || seen[it.ID] || it.Name["en"] == "" {
			return c, fmt.Errorf("bad catalog entry %q (%s)", it.ID, it.Kind)
		}
		seen[it.ID] = true
	}
	for _, m := range c.Models {
		if !modelRe.MatchString(m.Name) || m.MB <= 0 || seen[m.Name] {
			return c, fmt.Errorf("bad catalog model %q", m.Name)
		}
		seen[m.Name] = true
	}
	return c, nil
}

func loadCatalog() (catalog, error) {
	b, err := os.ReadFile(catalogFile)
	if err != nil {
		return catalog{}, err
	}
	return parseCatalog(b)
}

func (c catalog) item(id, kind string) (catItem, bool) {
	for _, it := range c.Items {
		if it.ID == id && it.Kind == kind {
			return it, true
		}
	}
	return catItem{}, false
}

// flathubFilter is flatpak's filter for the Flathub remote: every app is
// denied except the catalog's. Runtimes and extensions (Proton, the
// graphics drivers for Flatpak) stay allowed: the apps need them.
func flathubFilter(c catalog) string {
	var b strings.Builder
	b.WriteString("deny app/*\n")
	for _, it := range c.Items {
		if it.Kind == "flatpak" {
			fmt.Fprintf(&b, "allow app/%s\n", it.ID)
		}
	}
	return b.String()
}

func softDir(home string) string          { return filepath.Join(home, ".local", "share", "oros-soft") }
func softProgressFile(home string) string { return filepath.Join(home, "oros", "data", "soft.json") }
func flatpakUserDir(home string) string   { return filepath.Join(home, ".local", "share", "flatpak") }

type softProgress struct {
	ID      string `json:"id"`
	Action  string `json:"action"` // install, remove, update
	Step    string `json:"step"`
	Percent int    `json:"percent"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

func writeSoftProgress(file string, p softProgress) {
	b, _ := json.Marshal(p)
	if os.WriteFile(file+".tmp", b, 0o644) == nil {
		_ = os.Rename(file+".tmp", file)
	}
}

// flatpakInstalled lists the apps flatpak has in the home folder, with
// their version.
func flatpakInstalled() map[string]string {
	out, err := exec.Command(flatpakBin, "--user", "list", "--app", "--columns=application,version").Output()
	if err != nil {
		return map[string]string{}
	}
	return parseFlatpakList(string(out))
}

func parseFlatpakList(out string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\t")
		id := strings.TrimSpace(f[0])
		if !flatpakIDRe.MatchString(id) {
			continue
		}
		v := ""
		if len(f) > 1 {
			v = strings.TrimSpace(f[1])
		}
		m[id] = v
	}
	return m
}

// --- the native host's commands ---

// softList is "soft.list": the catalog with what is installed.
func softList() reply {
	c, err := loadCatalog()
	if err != nil {
		return fail(err, "")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	fl := flatpakInstalled()
	addons := readAddonList(addonListFile(home))
	var items []reply
	for _, it := range c.Items {
		r := reply{"id": it.ID, "kind": it.Kind, "section": it.Section, "name": it.Name, "summary": it.Summary}
		if it.Licence != "" {
			r["licence"] = it.Licence
		}
		if it.Home != "" {
			r["home"] = it.Home
		}
		if it.MB > 0 {
			r["mb"] = it.MB
		}
		switch it.Kind {
		case "flatpak":
			v, ok := fl[it.ID]
			r["installed"] = ok
			if ok && v != "" {
				r["version"] = v
			}
		case "addon":
			r["installed"] = addons[it.ID]
		case "builtin":
			r["installed"] = builtinInstalled(home, it.ID)
		}
		items = append(items, r)
	}
	var models []reply
	for _, m := range c.Models {
		models = append(models, reply{"name": m.Name, "mb": m.MB, "minMemGiB": m.MinMemGiB, "licence": m.Licence, "by": m.By, "summary": m.Summary})
	}
	r := readSoftProgress(home)
	r["items"] = items
	r["models"] = models
	r["persistent"] = persistenceTarget() != ""
	r["freeMB"] = freeMB(home)
	r["owner"] = ownerOnly()
	return r
}

func readSoftProgress(home string) reply {
	r := reply{"ok": true, "running": softBusy(home)}
	if b, err := os.ReadFile(softProgressFile(home)); err == nil {
		var p softProgress
		if json.Unmarshal(b, &p) == nil {
			r["progress"] = p
		}
	}
	return r
}

func builtinInstalled(home, id string) bool {
	switch id {
	case "ai":
		return aiInstalled(aiDir(home))
	case "claude":
		_, ok := claudeInstalled(claudeDir(home))
		return ok
	case "tor":
		_, err := os.Stat(torStartScript(torDir(home)))
		return err == nil
	case "nvidia":
		return readNvidiaState().File != ""
	}
	return false
}

func softBusy(home string) bool { return lockBusy(filepath.Join(softDir(home), ".lock")) }

// softStart is "soft.install", "soft.remove" and "soft.update": flatpak
// works in the background (soft --run), soft.list follows it.
func softStart(action, id string) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if action != "update" {
		c, err := loadCatalog()
		if err != nil {
			return fail(err, "")
		}
		it, ok := c.item(id, "flatpak")
		if !ok {
			return reply{"ok": false, "error": "not in the catalog"}
		}
		if action == "install" {
			if persistenceTarget() == "" {
				return reply{"ok": false, "error": "no persistence"}
			}
			if free := freeMB(home); free >= 0 && free < int64(it.MB)+512 {
				return reply{"ok": false, "error": "no space", "needMB": it.MB + 512, "freeMB": free}
			}
		}
	}
	if err := os.MkdirAll(softDir(home), 0o700); err != nil {
		return fail(err, "")
	}
	if softBusy(home) {
		return reply{"ok": true, "running": true}
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err, "")
	}
	args := []string{"soft", "--" + action}
	if id != "" {
		args = append(args, "--id", id)
	}
	writeSoftProgress(softProgressFile(home), softProgress{ID: id, Action: action, Step: "start"})
	cmd := exec.Command(self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("soft.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true, "running": true}
}

// softOpen is "soft.open": the program runs on labwc like Claude desktop
// (maximized), and orOS takes the whole screen again when it ends.
func softOpen(id string) reply {
	c, err := loadCatalog()
	if err != nil {
		return fail(err, "")
	}
	if _, ok := c.item(id, "flatpak"); !ok {
		return reply{"ok": false, "error": "not in the catalog"}
	}
	if _, ok := flatpakInstalled()[id]; !ok {
		return reply{"ok": false, "error": "not installed"}
	}
	cmd := exec.Command("/bin/sh", "-c", softShell, "oros-soft", flatpakBin, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("soft-run.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	return reply{"ok": true, "started": true}
}

const softShell = `"$1" run "$2"; command -v wlrctl >/dev/null && wlrctl toplevel maximize title:orOS; exit 0`

// --- oros-system soft (the background work, as the person) ---

func softMain(args []string) int {
	fl := flag.NewFlagSet("soft", flag.ContinueOnError)
	var install, remove, update, auto, filter bool
	var id string
	fl.BoolVar(&install, "install", false, "install a catalog app from Flathub")
	fl.BoolVar(&remove, "remove", false, "remove a catalog app")
	fl.BoolVar(&update, "update", false, "update every installed app")
	fl.BoolVar(&auto, "auto", false, "session start: the daily update")
	fl.BoolVar(&filter, "filter", false, "print the Flathub filter for the catalog")
	fl.StringVar(&id, "id", "", "the app (a catalog id)")
	cat := fl.String("catalog", catalogFile, "with --filter: the catalog to read (the build)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if filter {
		catalogFile = *cat
		c, err := loadCatalog()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(flathubFilter(c))
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if auto {
		softAuto(home)
		return 0
	}
	action := ""
	switch {
	case install:
		action = "install"
	case remove:
		action = "remove"
	case update:
		action = "update"
	default:
		fmt.Fprintln(os.Stderr, "usage: oros-system soft --install|--remove --id ID | --update | --auto | --filter")
		return 2
	}
	_ = os.MkdirAll(softDir(home), 0o700)
	lock, err := lockTake(filepath.Join(softDir(home), ".lock"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "another install is running")
		return 1
	}
	defer lock.Close()
	pf := softProgressFile(home)
	report := func(step string, pct int) {
		writeSoftProgress(pf, softProgress{ID: id, Action: action, Step: step, Percent: pct})
	}
	err = softRun(action, id, report)
	p := softProgress{ID: id, Action: action, Step: "done", Percent: 100, Done: true}
	if err != nil {
		p.Step, p.Percent, p.Error = "error", 0, err.Error()
		fmt.Fprintln(os.Stderr, err)
	}
	writeSoftProgress(pf, p)
	if err != nil {
		return 1
	}
	if action != "remove" {
		_ = os.WriteFile(filepath.Join(softDir(home), "updated"), []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
	}
	return 0
}

func softRun(action, id string, report func(string, int)) error {
	c, err := loadCatalog()
	if err != nil {
		return err
	}
	if action != "update" {
		if _, ok := c.item(id, "flatpak"); !ok {
			return errors.New("not in the catalog")
		}
	}
	switch action {
	case "install":
		report("remote", 1)
		if err := flathubRemote(); err != nil {
			return err
		}
		return flatpakRun(report, "install", "flathub", id)
	case "remove":
		if err := flatpakRun(report, "uninstall", id); err != nil {
			return err
		}
		// runtimes nothing else uses any more
		return flatpakRun(func(string, int) {}, "uninstall", "--unused")
	default:
		if err := flathubRemote(); err != nil {
			return err
		}
		return flatpakRun(report, "update")
	}
}

// flathubRemote adds Flathub to the home folder's flatpak, or sets its
// filter again: a new image may bring a longer catalog.
func flathubRemote() error {
	if _, err := os.Stat(flathubCheck); err != nil {
		return errors.New("this system has no Flathub filter")
	}
	out, err := run(flatpakBin, "--user", "remote-add", "--if-not-exists", "--filter="+flathubCheck, "flathub", flathubRepo)
	if err != nil {
		return cmdErrIf("flatpak remote-add", out, err)
	}
	out, err = run(flatpakBin, "--user", "remote-modify", "--filter="+flathubCheck, "flathub")
	return cmdErrIf("flatpak remote-modify", out, err)
}

// flatpakRun runs one flatpak transaction without questions and turns its
// output into a percentage: "Installing 2/5…" gives the step, a "37%" on
// the line the part of it.
func flatpakRun(report func(string, int), args ...string) error {
	full := append([]string{"--user", args[0], "--noninteractive", "-y"}, args[1:]...)
	cmd := exec.Command(flatpakBin, full...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var tail []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Split(scanLinesCR)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" {
				continue
			}
			fmt.Println(l)
			if len(tail) == 8 {
				tail = tail[1:]
			}
			tail = append(tail, l)
			if step, pct, ok := flatpakPercent(l); ok {
				report(step, pct)
			}
		}
	}()
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
	}
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("flatpak %s: %v: %s", args[0], err, strings.Join(tail, " | "))
	}
	return nil
}

var (
	flatpakStepRe = regexp.MustCompile(`^(Installing|Updating|Uninstalling)\D*?(\d+)/(\d+)`)
	flatpakPctRe  = regexp.MustCompile(`(\d{1,3})%`)
)

// flatpakPercent reads a progress line of flatpak's.
func flatpakPercent(l string) (string, int, bool) {
	m := flatpakStepRe.FindStringSubmatch(l)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[2])
	total, _ := strconv.Atoi(m[3])
	if total <= 0 || n <= 0 || n > total {
		return "", 0, false
	}
	part := 0
	if p := flatpakPctRe.FindAllStringSubmatch(l, -1); p != nil {
		part, _ = strconv.Atoi(p[len(p)-1][1])
		if part > 100 {
			part = 100
		}
	}
	pct := 2 + ((n-1)*100+part)*97/(total*100)
	return strings.ToLower(m[1]), pct, true
}

// scanLinesCR splits at \n and at \r (flatpak redraws its progress line).
func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// softAuto (session start): once a day, with the network up, the apps
// from the center update by themselves, with the Flathub filter of this
// image. Nothing happens when nothing was installed.
func softAuto(home string) {
	if err := gamesOverride(); err != nil {
		logf("games: flatpak override: %v", err)
	}
	if _, err := os.Stat(filepath.Join(flatpakUserDir(home), "app")); err != nil {
		return
	}
	if b, err := os.ReadFile(filepath.Join(softDir(home), "updated")); err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b))); err == nil && time.Since(t) < 20*time.Hour {
			return
		}
	}
	for i := 0; i < 60; i++ {
		if out, _ := run("nmcli", "-t", "-f", "CONNECTIVITY", "general"); strings.TrimSpace(out) == "full" {
			break
		}
		time.Sleep(10 * time.Second)
	}
	time.Sleep(2 * time.Minute) // let the session settle first
	self, err := os.Executable()
	if err != nil {
		return
	}
	if out, err := run(self, "soft", "--update"); err != nil {
		logf("soft: daily update: %v %s", err, lastLine(out))
	}
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return l[len(l)-1]
}

// sortedKeys is for stable output (tests, the policy file).
func sortedKeys(m map[string]bool) []string {
	var k []string
	for s, on := range m {
		if on {
			k = append(k, s)
		}
	}
	sort.Strings(k)
	return k
}

// selftestSoft (boot test, selftest-desk): the image's catalog, its Flathub
// filter and Flathub itself in a throwaway flatpak folder, where only the
// catalog's apps may be seen; and root's add-on policy step.
func selftestSoft(check func(string, error), info func(string, string)) {
	c, err := loadCatalog()
	check(fmt.Sprintf("software catalog (%d entries, %d models)", len(c.Items), len(c.Models)), err)
	if err != nil {
		return
	}
	b, err := os.ReadFile(flathubCheck)
	if err == nil && string(b) != flathubFilter(c) {
		err = errors.New("does not match the catalog")
	}
	check("Flathub filter", err)
	b, err = os.ReadFile(flathubRepo)
	if err == nil && (!strings.Contains(string(b), "\nUrl=https://dl.flathub.org/repo/") || !strings.Contains(string(b), "\nGPGKey=")) {
		err = errors.New("not Flathub's file")
	}
	check("flathub.flatpakrepo", err)

	dir, err := os.MkdirTemp("", "oros-flatpak")
	if err == nil {
		defer os.RemoveAll(dir)
		env := append(os.Environ(), "FLATPAK_USER_DIR="+dir, "LC_ALL=C")
		cmd := exec.Command(flatpakBin, "--user", "remote-add", "--filter="+flathubCheck, "flathub", flathubRepo)
		cmd.Env = env
		var out []byte
		out, err = cmd.CombinedOutput()
		err = cmdErrIf("remote-add", string(out), err)
		online := false
		for i := 0; i < 30 && err == nil; i++ {
			if o, _ := run("nmcli", "-t", "-f", "CONNECTIVITY", "general"); strings.TrimSpace(o) == "full" {
				online = true
				break
			}
			time.Sleep(3 * time.Second)
		}
		if err == nil && !online {
			info("Flathub through the filter", "skipped: no network")
		}
		if err == nil && online {
			cmd = exec.Command(flatpakBin, "--user", "remote-ls", "--app", "--columns=application", "flathub")
			cmd.Env = env
			out, err = cmd.CombinedOutput()
			err = cmdErrIf("remote-ls", string(out), err)
			if err == nil {
				seen := parseFlatpakList(string(out))
				for id := range seen {
					if _, ok := c.item(id, "flatpak"); !ok {
						err = fmt.Errorf("%s is not in the catalog", id)
					}
				}
				for _, it := range c.Items {
					if _, ok := seen[it.ID]; it.Kind == "flatpak" && !ok && err == nil {
						err = fmt.Errorf("%s is not on Flathub", it.ID)
					}
				}
				info("Flathub through the filter", strings.Join(sortedKeys(boolSet(seen)), " "))
			}
		}
	}
	check("Flathub shows only the catalog's apps", err)

	out, err := run("sudo", "-n", "/opt/oros/oros-system", "addons", "--apply")
	check("add-on policy (addons --apply)", cmdErrIf("addons", out, err))
}

func boolSet(m map[string]string) map[string]bool {
	s := map[string]bool{}
	for k := range m {
		s[k] = true
	}
	return s
}
