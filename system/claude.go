package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// Claude desktop (CLAUDE-1 wave C2): Anthropic's own app, opened over orOS
// like Tor Browser. The image does not carry it (Anthropic's software, not
// ours to pass on): when the person asks, "oros-system claude --fetch"
// reads Anthropic's apt repository the way apt does (the InRelease file
// signed by Anthropic's key, pinned here by fingerprint, then the
// Packages index and the .deb by the SHA-256 each one is listed with),
// unpacks the package into the home folder (the persistence partition,
// since the system itself starts fresh at every boot and with every
// system update) and starts it. Each user of the stick has a copy of
// their own, signed in with their own Claude account. Pressing install
// again later fetches a newer version when Anthropic has one.

const claudeRepo = "https://downloads.claude.ai/claude-desktop/apt/stable"

var (
	claudeKeyring = "/opt/oros/claude-desktop.gpg"
	// claudeFingerprint is Anthropic's Claude desktop repository key, as
	// Anthropic's install guide publishes it (scripts/claude-key.sh pins
	// the same); tests replace it.
	claudeFingerprint = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"
	claudeKeyURL      = "https://downloads.claude.ai/claude-desktop/key.asc"
	claudeHTTP        = &http.Client{Timeout: 30 * time.Minute}
	// claudeArch is Debian's name for this computer's processor.
	claudeArch = map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
)

const (
	claudeMaxIndex = 1 << 20
	claudeMaxPkgs  = 32 << 20
	claudeMaxDeb   = 900 << 20
)

func claudeDir(home string) string { return filepath.Join(home, ".local", "share", "oros-claude") }
func claudeProgressFile(home string) string {
	return filepath.Join(home, "oros", "data", "claude.json")
}
func claudeBusy(dir string) bool { return lockBusy(filepath.Join(dir, ".lock")) }

// claudeApp is what a finished download leaves in <dir>/app.json.
type claudeApp struct {
	Version string `json:"version"`
	Exec    string `json:"exec"` // relative to <dir>/root
}

func claudeInstalled(dir string) (claudeApp, bool) {
	var a claudeApp
	b, err := os.ReadFile(filepath.Join(dir, "app.json"))
	if err != nil || json.Unmarshal(b, &a) != nil || !claudeRelOK(a.Exec) {
		return a, false
	}
	if fi, err := os.Stat(filepath.Join(dir, "root", a.Exec)); err != nil || !fi.Mode().IsRegular() {
		return a, false
	}
	return a, true
}

func claudeRelOK(p string) bool {
	return p != "" && !filepath.IsAbs(p) && !strings.HasPrefix(filepath.Clean(p), "..")
}

// ---------- native host commands ----------

func claudeStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := claudeDir(home)
	r := readProgress(claudeProgressFile(home))
	r["running"] = claudeBusy(dir)
	a, ok := claudeInstalled(dir)
	r["installed"] = ok
	if ok {
		r["version"] = a.Version
	}
	r["persistent"] = persistenceTarget() != ""
	r["supported"] = claudeArch != ""
	return r
}

// claudeInstall starts the download in the background; the panel and
// orOS's Settings follow it with claude.status.
func claudeInstall() reply {
	if claudeArch == "" {
		return reply{"ok": false, "error": "os: this processor has no Claude desktop"}
	}
	// Without a persistence partition the home folder lives in memory:
	// the app would be gone at the next start.
	if persistenceTarget() == "" {
		return reply{"ok": false, "error": "persist: this stick has no persistence partition"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := claudeDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err, "")
	}
	if claudeBusy(dir) {
		return reply{"ok": true, "running": true}
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err, "")
	}
	_ = os.Remove(claudeProgressFile(home))
	cmd := exec.Command(self, "claude", "--fetch")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("claude.log"); f != nil {
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

func claudeOpen() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if err := startClaude(claudeDir(home)); err != nil {
		return fail(err, "")
	}
	return reply{"ok": true, "started": true}
}

// claudeRemove deletes the program. The person's sign-in and chats on
// this computer (~/.config/Claude) stay, as after uninstalling any app.
func claudeRemove() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := claudeDir(home)
	if claudeBusy(dir) {
		return fail(errors.New("Claude desktop is being downloaded"), "")
	}
	_ = os.Remove(filepath.Join(dir, "app.json"))
	if err := os.RemoveAll(filepath.Join(dir, "root")); err != nil {
		return fail(err, "")
	}
	_ = os.RemoveAll(filepath.Join(dir, "root.old"))
	_ = os.Remove(claudeProgressFile(home))
	return reply{"ok": true}
}

// startClaude runs Claude desktop detached on labwc's Wayland. labwc puts
// it on the right half of the screen and the orOS window on the left
// (/etc/xdg/oros-labwc/rc.xml, Chris 2026-11-16 «Claude δίπλα στο orOS»);
// when Claude closes, the small shell around it gives orOS the whole
// screen back (wlrctl, through labwc's foreign-toplevel protocol).
func startClaude(dir string) error {
	a, ok := claudeInstalled(dir)
	if !ok {
		return errors.New("Claude desktop is not installed")
	}
	bin := filepath.Join(dir, "root", a.Exec)
	cmd := exec.Command("/bin/sh", "-c", claudeShell, "oros-claude", bin)
	cmd.Dir = filepath.Dir(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("claude-run.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// claudeShell: $1 is Claude's program. orOS takes the whole screen again
// when it ends (nothing happens where wlrctl is missing).
const claudeShell = `"$1" --ozone-platform=wayland; command -v wlrctl >/dev/null && wlrctl toplevel maximize title:orOS; exit 0`

// ---------- oros-system claude --fetch ----------

type claudeOpts struct {
	dir, repo, keyring, progress, arch string
	start                              bool
	// noLibCheck skips ldd: CI checks the download on a build machine,
	// not on the image.
	noLibCheck bool
}

func claudeMain(args []string) int {
	fl := flag.NewFlagSet("claude", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	o := claudeOpts{arch: claudeArch}
	var fetch, noStart bool
	fl.BoolVar(&fetch, "fetch", false, "download, check and unpack Anthropic's Claude desktop")
	fl.StringVar(&o.dir, "dir", claudeDir(home), "folder for Claude desktop")
	fl.StringVar(&o.repo, "repo", claudeRepo, "Anthropic's apt repository")
	fl.StringVar(&o.keyring, "keyring", claudeKeyring, "keyring with the repository's signing key")
	fl.StringVar(&o.progress, "progress", claudeProgressFile(home), "progress file (JSON)")
	fl.BoolVar(&noStart, "no-start", false, "do not start Claude desktop afterwards")
	fl.BoolVar(&o.noLibCheck, "no-lib-check", false, "do not check this system's libraries (CI)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if !fetch {
		fmt.Fprintln(os.Stderr, "usage: oros-system claude --fetch [--dir D] [--no-start]")
		return 2
	}
	o.start = !noStart
	report := progressWriter(o.progress)
	if err := claudeFetch(o, func(step string, pct int) { report(progress{Step: step, Percent: pct}) }); err != nil {
		report(progress{Step: "error", Done: true, Error: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

func claudeGet(u string, max int64, out io.Writer, step func(n, total int64)) error {
	resp, err := claudeHTTP.Get(u)
	if err != nil {
		return fmt.Errorf("net: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	if resp.ContentLength > max {
		return fmt.Errorf("%s: too large", u)
	}
	buf := make([]byte, 1<<20)
	var n int64
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
			if step != nil {
				step(n, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("net: %s: %v", u, rerr)
		}
	}
}

// claudeKeys keeps only the key with the pinned fingerprint.
func claudeKeys(b []byte) (openpgp.EntityList, error) {
	l, err := openpgp.ReadKeyRing(bytes.NewReader(b))
	if err != nil {
		l, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	}
	if err != nil {
		return nil, fmt.Errorf("Claude desktop's signing key: %v", err)
	}
	for _, e := range l {
		if e.PrimaryKey != nil && strings.EqualFold(fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), claudeFingerprint) {
			return openpgp.EntityList{e}, nil
		}
	}
	return nil, errors.New("Claude desktop's signing key has an unexpected fingerprint")
}

// claudeRelease checks InRelease's signature and returns its signed text.
// The image's key comes first; when it cannot check (a renewed subkey),
// Anthropic's published key is fetched and used only with the same
// fingerprint.
func claudeRelease(inRelease []byte, keyring string) ([]byte, error) {
	block, _ := clearsign.Decode(inRelease)
	if block == nil {
		return nil, errors.New("the repository's InRelease is not signed")
	}
	check := func(key openpgp.EntityList) error {
		_, err := openpgp.CheckDetachedSignature(key, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
		return err
	}
	var first error
	if b, err := os.ReadFile(keyring); err == nil {
		if key, err := claudeKeys(b); err == nil {
			if first = check(key); first == nil {
				return block.Plaintext, nil
			}
			if !torKeyProblem(first) {
				return nil, fmt.Errorf("signature check failed: %v", first)
			}
		} else {
			first = err
		}
	}
	// the signature was read once: decode again for the second check
	block, _ = clearsign.Decode(inRelease)
	var kb bytes.Buffer
	if err := claudeGet(claudeKeyURL, 1<<20, &kb, nil); err != nil {
		if first != nil {
			return nil, fmt.Errorf("signature check failed: %v", first)
		}
		return nil, err
	}
	key, err := claudeKeys(kb.Bytes())
	if err != nil {
		return nil, err
	}
	if err := check(key); err != nil {
		return nil, fmt.Errorf("signature check failed: %v", err)
	}
	return block.Plaintext, nil
}

type claudeFile struct {
	sum  string
	size int64
}

// releaseFields reads a Release file: its single-line fields and the
// SHA256 list (path → hash and size).
func releaseFields(text []byte) (map[string]string, map[string]claudeFile) {
	fields := map[string]string{}
	files := map[string]claudeFile{}
	inSHA := false
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") {
			if inSHA {
				f := strings.Fields(line)
				if len(f) == 3 {
					if n, err := strconv.ParseInt(f[1], 10, 64); err == nil && len(f[0]) == 64 {
						files[f[2]] = claudeFile{strings.ToLower(f[0]), n}
					}
				}
			}
			continue
		}
		inSHA = false
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if k == "SHA256" {
			inSHA = true
			continue
		}
		fields[k] = strings.TrimSpace(v)
	}
	return fields, files
}

type claudePkg struct {
	version, filename, sum string
	size                   int64
}

var claudePoolRe = regexp.MustCompile(`^pool/[A-Za-z0-9._+~/-]+\.deb$`)

// claudePick returns the newest claude-desktop for arch in a Packages
// index.
func claudePick(index []byte, arch string) (claudePkg, error) {
	var best claudePkg
	for _, st := range bytes.Split(index, []byte("\n\n")) {
		f := map[string]string{}
		for _, line := range strings.Split(string(st), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") {
				f[k] = strings.TrimSpace(v)
			}
		}
		if f["Package"] != "claude-desktop" || (f["Architecture"] != arch && f["Architecture"] != "all") {
			continue
		}
		size, err := strconv.ParseInt(f["Size"], 10, 64)
		sum := strings.ToLower(f["SHA256"])
		fn := f["Filename"]
		if err != nil || size <= 0 || len(sum) != 64 || !claudePoolRe.MatchString(fn) || strings.Contains(fn, "..") || f["Version"] == "" {
			continue
		}
		if best.version == "" || debCompare(f["Version"], best.version) > 0 {
			best = claudePkg{f["Version"], fn, sum, size}
		}
	}
	if best.version == "" {
		return best, fmt.Errorf("os: no Claude desktop for %s in Anthropic's repository", arch)
	}
	return best, nil
}

func claudeFetch(o claudeOpts, step func(string, int)) error {
	if o.arch == "" {
		return errors.New("os: this processor has no Claude desktop")
	}
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(o.dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("Claude desktop is already being downloaded")
	}

	step("index", 0)
	var ir bytes.Buffer
	if err := claudeGet(o.repo+"/dists/stable/InRelease", claudeMaxIndex, &ir, nil); err != nil {
		return err
	}
	text, err := claudeRelease(ir.Bytes(), o.keyring)
	if err != nil {
		return err
	}
	fields, files := releaseFields(text)
	if vu := fields["Valid-Until"]; vu != "" {
		if t, err := time.Parse(time.RFC1123, strings.Replace(vu, "UTC", "GMT", 1)); err == nil && time.Now().After(t) {
			return errors.New("Anthropic's repository index has expired")
		}
	}
	var pkgs []byte
	for _, name := range []string{"main/binary-" + o.arch + "/Packages", "main/binary-" + o.arch + "/Packages.gz"} {
		want, ok := files[name]
		if !ok {
			continue
		}
		var b bytes.Buffer
		if err := claudeGet(o.repo+"/dists/stable/"+name, claudeMaxPkgs, &b, nil); err != nil {
			return err
		}
		if err := claudeSum(b.Bytes(), want); err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		pkgs = b.Bytes()
		if strings.HasSuffix(name, ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(pkgs))
			if err != nil {
				return err
			}
			pkgs, err = io.ReadAll(io.LimitReader(zr, claudeMaxPkgs))
			if err != nil {
				return err
			}
		}
		break
	}
	if pkgs == nil {
		return fmt.Errorf("os: Anthropic's repository lists no packages for %s", o.arch)
	}
	pkg, err := claudePick(pkgs, o.arch)
	if err != nil {
		return err
	}
	if pkg.size > claudeMaxDeb {
		return errors.New("the Claude desktop package is unexpectedly large")
	}
	if a, ok := claudeInstalled(o.dir); ok && a.Version == pkg.version {
		fmt.Printf("Claude desktop %s is up to date\n", pkg.version)
		if o.start {
			step("start", 99)
			return startClaude(o.dir)
		}
		return nil
	}
	fmt.Printf("Claude desktop %s: %s\n", pkg.version, pkg.filename)

	deb := filepath.Join(o.dir, "download.deb")
	defer os.Remove(deb)
	f, err := os.Create(deb)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = claudeGet(o.repo+"/"+pkg.filename, pkg.size, io.MultiWriter(f, h), func(n, total int64) {
		step("download", int(n*90/pkg.size))
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	step("verify", 92)
	if got := hex.EncodeToString(h.Sum(nil)); got != pkg.sum {
		return errors.New("the Claude desktop download does not match Anthropic's signed index")
	}

	step("unpack", 95)
	tmp := filepath.Join(o.dir, "new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.Command("dpkg-deb", "-x", deb, tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking Claude desktop: %v %s", err, strings.TrimSpace(string(out)))
	}
	rel, err := claudeFindExec(tmp)
	if err != nil {
		return err
	}
	if missing := claudeMissingLibs(filepath.Join(tmp, rel)); len(missing) > 0 && !o.noLibCheck {
		return fmt.Errorf("os: this system lacks libraries Claude desktop needs: %s", strings.Join(missing, ", "))
	}
	cur := filepath.Join(o.dir, "root")
	old := filepath.Join(o.dir, "root.old")
	_ = os.Remove(filepath.Join(o.dir, "app.json"))
	_ = os.RemoveAll(old)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	_ = os.RemoveAll(old)
	b, _ := json.Marshal(claudeApp{Version: pkg.version, Exec: rel})
	if err := os.WriteFile(filepath.Join(o.dir, "app.json"), b, 0o600); err != nil {
		return err
	}
	if o.start {
		step("start", 99)
		return startClaude(o.dir)
	}
	return nil
}

func claudeSum(b []byte, want claudeFile) error {
	s := sha256.Sum256(b)
	if int64(len(b)) != want.size || hex.EncodeToString(s[:]) != want.sum {
		return errors.New("does not match Anthropic's signed index")
	}
	return nil
}

// claudeFindExec finds the program in the unpacked package: the target of
// /usr/bin/claude-desktop or of the menu entry's Exec when that is a
// program (ELF), otherwise the Electron program next to resources/app.asar.
// Links are followed inside root only. It returns the path relative to
// root.
func claudeFindExec(root string) (string, error) {
	cands := []string{"/usr/bin/claude-desktop"}
	if ds, _ := filepath.Glob(filepath.Join(root, "usr/share/applications/*.desktop")); len(ds) > 0 {
		for _, d := range ds {
			b, err := os.ReadFile(d)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(line, "Exec="); ok {
					if f := strings.Fields(v); len(f) > 0 {
						p := strings.Trim(f[0], `"`)
						if !filepath.IsAbs(p) {
							p = "/usr/bin/" + p
						}
						cands = append(cands, p)
					}
					break
				}
			}
		}
	}
	for _, c := range cands {
		if rel, ok := claudeResolve(root, c); ok && isELF(filepath.Join(root, rel)) {
			return rel, nil
		}
	}
	var found string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" || !d.IsDir() || filepath.Base(p) != "resources" {
			return nil
		}
		if _, err := os.Stat(filepath.Join(p, "app.asar")); err != nil {
			return nil
		}
		ents, _ := os.ReadDir(filepath.Dir(p))
		for _, e := range ents {
			q := filepath.Join(filepath.Dir(p), e.Name())
			fi, err := os.Lstat(q)
			if err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 && strings.Contains(strings.ToLower(e.Name()), "claude") && isELF(q) {
				found = q
				return filepath.SkipAll
			}
		}
		return nil
	})
	if found == "" {
		return "", errors.New("the Claude desktop package has an unexpected layout")
	}
	rel, _ := filepath.Rel(root, found)
	return rel, nil
}

// claudeResolve follows symbolic links of the absolute path p as if root
// were /, and never leaves root. It returns a path relative to root.
func claudeResolve(root, p string) (string, bool) {
	cur := filepath.Clean("/" + p)
	for i := 0; i < 16; i++ {
		full := filepath.Join(root, cur)
		fi, err := os.Lstat(full)
		if err != nil {
			return "", false
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			if !fi.Mode().IsRegular() || fi.Mode()&0o111 == 0 {
				return "", false
			}
			return strings.TrimPrefix(cur, "/"), true
		}
		t, err := os.Readlink(full)
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(cur), t)
		}
		cur = filepath.Clean("/" + t)
	}
	return "", false
}

func isELF(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4)
	_, err = io.ReadFull(f, b)
	return err == nil && string(b) == "\x7fELF"
}

// claudeMissingLibs lists the shared libraries the program needs that
// this system lacks (ldd's "not found"), with the package's own folder on
// the search path.
func claudeMissingLibs(bin string) []string {
	cmd := exec.Command("ldd", bin)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(bin))
	out, _ := cmd.Output()
	var miss []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "not found") {
			if f := strings.Fields(line); len(f) > 0 {
				miss = append(miss, f[0])
			}
		}
	}
	return miss
}

// debCompare compares two Debian version strings like dpkg does
// (epoch:upstream-revision, "~" before everything, letters before other
// characters).
func debCompare(a, b string) int {
	ea, ua, ra := debSplit(a)
	eb, ub, rb := debSplit(b)
	if ea != eb {
		if ea < eb {
			return -1
		}
		return 1
	}
	if c := debPart(ua, ub); c != 0 {
		return c
	}
	return debPart(ra, rb)
}

func debSplit(v string) (int, string, string) {
	e := 0
	if i := strings.IndexByte(v, ':'); i >= 0 {
		e, _ = strconv.Atoi(v[:i])
		v = v[i+1:]
	}
	r := ""
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		r = v[i+1:]
		v = v[:i]
	}
	return e, v, r
}

func debOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case c >= '0' && c <= '9':
		return 0
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return int(c)
	default:
		return int(c) + 256
	}
}

func debPart(a, b string) int {
	for a != "" || b != "" {
		for (a != "" && (a[0] < '0' || a[0] > '9')) || (b != "" && (b[0] < '0' || b[0] > '9')) {
			var ca, cb int
			if a != "" && (a[0] < '0' || a[0] > '9') {
				ca = debOrder(a[0])
			}
			if b != "" && (b[0] < '0' || b[0] > '9') {
				cb = debOrder(b[0])
			}
			if ca != cb {
				if ca < cb {
					return -1
				}
				return 1
			}
			if a != "" {
				a = a[1:]
			}
			if b != "" {
				b = b[1:]
			}
		}
		i, j := 0, 0
		for i < len(a) && a[i] >= '0' && a[i] <= '9' {
			i++
		}
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j++
		}
		na := strings.TrimLeft(a[:i], "0")
		nb := strings.TrimLeft(b[:j], "0")
		if len(na) != len(nb) {
			if len(na) < len(nb) {
				return -1
			}
			return 1
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
		a, b = a[i:], b[j:]
	}
	return 0
}
