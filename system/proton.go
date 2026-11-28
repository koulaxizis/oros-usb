package main

// GE-Proton in one click (Chris 2026-11-27, evolution idea 7: «Ναι, και
// αυτόματα»). GE-Proton is Proton with extra game fixes, often the ones a
// game on an NVIDIA card needs. ProtonPlus (in the catalog) can manage
// many versions by hand; Settings › Software adds what a person without
// those details needs: one button, "Update GE-Proton", and a switch, off
// by default, that downloads a new GE-Proton by itself when it comes out.
//
// It does what ProtonPlus does for Steam from Flathub: the newest release
// of GloriousEggroll/proton-ge-custom from GitHub, its .tar.gz checked
// against the release's own .sha512sum, unpacked into Steam's
// compatibilitytools.d. There it has one fixed name, "GE-Proton (orOS)",
// so a game set to it once always runs the newest GE-Proton, an update
// replaces the folder in place (unpacked beside it first, then swapped)
// and the stick never fills with old versions. Versions ProtonPlus
// installed keep their own folders and are never touched.
//
// The checksum comes from the same release, so it catches a broken
// download, not a forged release; GE-Proton publishes no signature.

import (
	"bufio"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	protonID      = "ge-proton"      // its progress in soft.json
	protonFolder  = "GE-Proton-orOS" // its folder in compatibilitytools.d
	protonName    = "GE-Proton-orOS" // Steam's internal name: never changes
	protonMaxGz   = 2 << 30
	protonNeedMB  = 3072 // download + unpacked copy + the old one for a moment
	protonDailyAt = 20 * time.Hour
)

var (
	protonLatestURL = "https://api.github.com/repos/GloriousEggroll/proton-ge-custom/releases/latest"
	protonTagRe     = regexp.MustCompile(`^GE-Proton[0-9]{1,3}-[0-9]{1,4}$`)
	protonHTTP      = &http.Client{Timeout: 45 * time.Minute, CheckRedirect: protonRedirect}
)

// Steam from Flathub keeps its data here (ProtonPlus uses the same place).
func protonToolsDir(home string) string {
	return filepath.Join(home, ".var", "app", steamID, "data", "Steam", "compatibilitytools.d")
}
func protonFile(home string) string { return filepath.Join(home, "oros", "data", "geproton.json") }

type protonSettings struct {
	Auto    bool   `json:"auto"`
	Version string `json:"version,omitempty"` // the GE-Proton in protonFolder
	Checked string `json:"checked,omitempty"` // last look at GitHub (RFC 3339)
}

func readProton(home string) protonSettings {
	var s protonSettings
	if b, err := os.ReadFile(protonFile(home)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if !protonTagRe.MatchString(s.Version) {
		s.Version = ""
	}
	return s
}

func writeProton(home string, s protonSettings) error {
	f := protonFile(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	if err := os.WriteFile(f+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(f+".tmp", f)
}

// protonInstalled: the version in Steam's folder, or "" (a folder removed
// by hand, or by ProtonPlus, counts as none).
func protonInstalled(home string) string {
	s := readProton(home)
	if s.Version == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(protonToolsDir(home), protonFolder, "proton")); err != nil {
		return ""
	}
	return s.Version
}

// --- the native host's commands ---

// protonStatus is "proton.status".
func protonStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	s := readProton(home)
	r := reply{"ok": true, "steam": steamInstalled(), "auto": s.Auto, "running": softBusy(home)}
	if v := protonInstalled(home); v != "" {
		r["version"] = v
	}
	if s.Checked != "" {
		r["checked"] = s.Checked
	}
	return r
}

// protonStart is "proton.update" (owner only): the work runs in the
// background under the software center's lock, soft.list follows it.
func protonStart() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if !steamInstalled() {
		return reply{"ok": false, "error": "steam missing"}
	}
	if persistenceTarget() == "" {
		return reply{"ok": false, "error": "no persistence"}
	}
	if free := freeMB(home); free >= 0 && free < protonNeedMB {
		return reply{"ok": false, "error": "no space", "needMB": protonNeedMB, "freeMB": free}
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
	writeSoftProgress(softProgressFile(home), softProgress{ID: protonID, Action: "update", Step: "start"})
	cmd := exec.Command(self, "proton", "--update")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("proton.log"); f != nil {
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

// protonSetAuto is "proton.auto" (owner only).
func protonSetAuto(on bool) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	s := readProton(home)
	s.Auto = on
	if err := writeProton(home, s); err != nil {
		return fail(err, "")
	}
	return protonStatus()
}

// protonRemove is "proton.remove" (owner only): only orOS's own folder.
func protonRemove() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if softBusy(home) {
		return reply{"ok": false, "error": "busy"}
	}
	if err := os.RemoveAll(filepath.Join(protonToolsDir(home), protonFolder)); err != nil {
		return fail(err, "")
	}
	s := readProton(home)
	s.Version = ""
	if err := writeProton(home, s); err != nil {
		return fail(err, "")
	}
	return protonStatus()
}

// --- oros-system proton --update (as the person) ---

func protonMain(args []string) int {
	fl := flag.NewFlagSet("proton", flag.ContinueOnError)
	var update bool
	fl.BoolVar(&update, "update", false, "download the newest GE-Proton into Steam when it is newer")
	if err := fl.Parse(args); err != nil || !update {
		fmt.Fprintln(os.Stderr, "usage: oros-system proton --update")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
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
		writeSoftProgress(pf, softProgress{ID: protonID, Action: "update", Step: step, Percent: pct})
	}
	v, err := protonUpdate(home, protonLatestURL, report)
	p := softProgress{ID: protonID, Action: "update", Step: "done", Percent: 100, Done: true}
	if err != nil {
		p.Step, p.Percent, p.Error = "error", 0, err.Error()
		fmt.Fprintln(os.Stderr, err)
	} else {
		fmt.Println("GE-Proton:", v)
	}
	writeSoftProgress(pf, p)
	if err != nil {
		return 1
	}
	return 0
}

type protonRelease struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// protonPick reads GitHub's answer for the latest release: its version,
// the archive and its checksum file, both on github.com.
func protonPick(b []byte) (tag, gz, sum string, size int64, err error) {
	var r protonRelease
	if err := json.Unmarshal(b, &r); err != nil {
		return "", "", "", 0, fmt.Errorf("GE-Proton release: %v", err)
	}
	if r.Draft || r.Pre || !protonTagRe.MatchString(r.Tag) {
		return "", "", "", 0, fmt.Errorf("GE-Proton release %q is not a release", r.Tag)
	}
	// Since GE-Proton11 each release has one archive per processor,
	// "<tag>-x86_64.tar.gz" with "<tag>-x86_64.sha512sum"; older ones
	// "<tag>.tar.gz" with "<tag>.sha512sum". The archive picks its checksum.
	var names []string
	base := ""
	for _, pre := range []string{r.Tag + "-" + protonArch(), r.Tag} {
		for _, a := range r.Assets {
			if gz == "" && protonArchive(pre, a.Name) {
				gz, size, base = a.URL, a.Size, pre
			}
		}
	}
	for _, a := range r.Assets {
		names = append(names, a.Name)
		if base != "" && a.Name == base+".sha512sum" {
			sum = a.URL
		}
	}
	if gz == "" || sum == "" {
		return "", "", "", 0, fmt.Errorf("GE-Proton %s: archive or checksum missing for %s (%s)", r.Tag, protonArch(), strings.Join(names, ", "))
	}
	for _, u := range []string{gz, sum} {
		if !protonURL(u) {
			return "", "", "", 0, fmt.Errorf("unexpected download address %q", u)
		}
	}
	if size <= 0 || size > protonMaxGz {
		return "", "", "", 0, fmt.Errorf("GE-Proton %s: size %d", r.Tag, size)
	}
	return r.Tag, gz, sum, size, nil
}

// protonArch: GE-Proton's name for this processor.
func protonArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

// protonArchive: the release's archive, "<tag>.tar.gz" (or .tar.xz,
// .tar.zst, which tar unpacks the same way).
func protonArchive(tag, name string) bool {
	for _, ext := range []string{".tar.gz", ".tar.xz", ".tar.zst"} {
		if name == tag+ext {
			return true
		}
	}
	return false
}

// protonURL: https on GitHub only (the archives redirect to GitHub's
// download hosts).
func protonURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "github.com" || h == "api.github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

func protonRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 5 {
		return errors.New("too many redirects")
	}
	if !protonURL(req.URL.String()) {
		return fmt.Errorf("redirect to %s refused", req.URL.Host)
	}
	return nil
}

func protonGet(u string, max int64, out io.Writer, progress func(n, total int64)) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "orOS-USB")
	if strings.HasPrefix(u, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		// CI only: runners share addresses and GitHub's rate limit
		if tok := os.Getenv("OROS_GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := protonHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	if resp.ContentLength > max {
		return fmt.Errorf("%s: too large", u)
	}
	var n int64
	buf := make([]byte, 1<<20)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			n += int64(k)
			if n > max {
				return fmt.Errorf("%s: too large", u)
			}
			if _, err := out.Write(buf[:k]); err != nil {
				return err
			}
			if progress != nil {
				progress(n, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("%s: %v", u, rerr)
		}
	}
}

// protonSum finds the archive's SHA-512 in the release's .sha512sum: the
// line naming the archive, or the file's only line (it belongs to this
// archive alone, and some name the archive without the processor).
func protonSum(b []byte, file string) (string, error) {
	var only []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || len(f[0]) != 128 {
			continue
		}
		if _, err := hex.DecodeString(f[0]); err != nil {
			continue
		}
		if strings.TrimPrefix(f[1], "*") == file {
			return strings.ToLower(f[0]), nil
		}
		only = append(only, strings.ToLower(f[0]))
	}
	if len(only) == 1 {
		return only[0], nil
	}
	return "", fmt.Errorf("no checksum for %s", file)
}

// protonVDF is Steam's description of the tool, with orOS's fixed name.
func protonVDF(version string) string {
	return `"compatibilitytools"
{
  "compat_tools"
  {
    "` + protonName + `"
    {
      "install_path" "."
      "display_name" "GE-Proton (orOS) ` + strings.TrimPrefix(version, "GE-Proton") + `"
      "from_oslist"  "windows"
      "to_oslist"    "linux"
    }
  }
}
`
}

// protonUpdate brings the newest GE-Proton into Steam when it is newer
// than the one there; it returns the version Steam now has.
func protonUpdate(home, latest string, step func(string, int)) (string, error) {
	s := readProton(home)
	step("check", 1)
	var idx strings.Builder
	if err := protonGet(latest, 1<<20, &idx, nil); err != nil {
		return "", err
	}
	tag, gzURL, sumURL, size, err := protonPick([]byte(idx.String()))
	if err != nil {
		return "", err
	}
	s.Checked = time.Now().UTC().Format(time.RFC3339)
	if have := protonInstalled(home); have == tag {
		_ = writeProton(home, s)
		return tag, nil
	}
	if free := freeMB(home); free >= 0 && free < size>>20*3+512 {
		return "", fmt.Errorf("no space: %d MB free", free)
	}

	tools := protonToolsDir(home)
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return "", err
	}
	var sumText strings.Builder
	if err := protonGet(sumURL, 4096, &sumText, nil); err != nil {
		return "", err
	}
	want, err := protonSum([]byte(sumText.String()), path.Base(gzURL))
	if err != nil {
		return "", err
	}
	work := filepath.Join(tools, ".oros-new")
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	file := filepath.Join(work, "download"+strings.TrimPrefix(path.Base(gzURL), tag))
	f, err := os.Create(file)
	if err != nil {
		return "", err
	}
	h := sha512.New()
	err = protonGet(gzURL, protonMaxGz, io.MultiWriter(f, h), func(n, total int64) {
		if total > 0 {
			step("download", 2+int(n*88/total))
		}
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	step("verify", 91)
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("GE-Proton %s: the download does not match its checksum", tag)
	}

	step("unpack", 93)
	out := filepath.Join(work, "x")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	if o, err := exec.Command("tar", "-xf", file, "-C", out, "--no-same-owner").CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpacking GE-Proton: %v %s", err, strings.TrimSpace(string(o)))
	}
	_ = os.Remove(file)
	got := filepath.Join(out, tag)
	if _, err := os.Stat(got); err != nil {
		got = filepath.Join(out, tag+"-"+protonArch())
	}
	for _, need := range []string{"proton", "compatibilitytool.vdf", "files"} {
		if _, err := os.Stat(filepath.Join(got, need)); err != nil {
			return "", fmt.Errorf("GE-Proton %s has an unexpected layout (%s)", tag, need)
		}
	}
	if err := os.WriteFile(filepath.Join(got, "compatibilitytool.vdf"), []byte(protonVDF(tag)), 0o644); err != nil {
		return "", err
	}

	step("install", 98)
	cur := filepath.Join(tools, protonFolder)
	old := filepath.Join(work, "old")
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return "", err
		}
	}
	if err := os.Rename(got, cur); err != nil {
		_ = os.Rename(old, cur)
		return "", err
	}
	s.Version = tag
	return tag, writeProton(home, s)
}

// protonAutoDue: at the session start, once a day, when the owner turned
// automatic updates on and Steam is there.
func protonAutoDue(home string) bool {
	s := readProton(home)
	if !s.Auto || !steamInstalled() || persistenceTarget() == "" {
		return false
	}
	if t, err := time.Parse(time.RFC3339, s.Checked); err == nil && time.Since(t) < protonDailyAt {
		return false
	}
	return true
}
