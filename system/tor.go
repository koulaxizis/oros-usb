package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The Tor button opens the official Tor Browser over orOS. The image does
// not carry it: the first press downloads it from torproject.org into the
// home folder (the persistence partition), checks its signature against
// Tor Browser's signing key that the build pinned by fingerprint
// (scripts/tor-key.sh), and unpacks it. From then on it starts at once and
// keeps itself up to date like on any computer.

const torIndex = "https://aus1.torproject.org/torbrowser/update_3/release/downloads.json"

var (
	torKeyring = "/opt/oros/tor-browser-developers.gpg"
	// torVerify checks the detached signature sig of file; tests replace it.
	torVerify = func(keyring, sig, file string) error {
		out, err := exec.Command("gpgv", "--keyring", keyring, sig, file).CombinedOutput()
		if err != nil {
			return fmt.Errorf("signature check failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
)

const (
	torMaxDownload = 600 << 20
	torMaxSig      = 64 << 10
)

func torDir(home string) string { return filepath.Join(home, ".local", "share", "oros-tor") }

func torStartScript(dir string) string {
	return filepath.Join(dir, "tor-browser", "Browser", "start-tor-browser")
}

func torProgressFile(home string) string { return filepath.Join(home, "oros", "data", "tor.json") }

// torBusy is true while a download holds the lock on the Tor folder.
func torBusy(dir string) bool {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// openTor starts Tor Browser, or the download that installs it first.
func openTor() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := torDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err, "")
	}
	persistent := persistenceTarget() != ""
	if torBusy(dir) {
		return reply{"ok": true, "downloading": true, "persistent": persistent}
	}
	if _, err := os.Stat(torStartScript(dir)); err == nil {
		if err := startTor(dir); err != nil {
			return fail(err, "")
		}
		return reply{"ok": true, "started": true}
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err, "")
	}
	_ = os.Remove(torProgressFile(home))
	cmd := exec.Command(self, "tor", "--fetch")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("tor.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	// Let the download take its lock before the next status call.
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true, "downloading": true, "persistent": persistent}
}

func torStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := torDir(home)
	r := readProgress(torProgressFile(home))
	r["running"] = torBusy(dir)
	_, err = os.Stat(torStartScript(dir))
	r["installed"] = err == nil
	r["persistent"] = persistenceTarget() != ""
	return r
}

// startTor runs Tor Browser detached; cage shows it over orOS and closing
// it brings orOS back.
func startTor(dir string) error {
	cmd := exec.Command(torStartScript(dir), "--detach")
	cmd.Dir = filepath.Dir(torStartScript(dir))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

type torOpts struct {
	dir, index, keyring, progress string
	start                         bool
}

func torMain(args []string) int {
	fl := flag.NewFlagSet("tor", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	o := torOpts{}
	var fetch, noStart bool
	fl.BoolVar(&fetch, "fetch", false, "download, check and unpack the latest Tor Browser")
	fl.StringVar(&o.dir, "dir", torDir(home), "folder for Tor Browser")
	fl.StringVar(&o.index, "index", torIndex, "Tor Browser's downloads.json")
	fl.StringVar(&o.keyring, "keyring", torKeyring, "keyring with Tor Browser's signing key")
	fl.StringVar(&o.progress, "progress", torProgressFile(home), "progress file (JSON)")
	fl.BoolVar(&noStart, "no-start", false, "do not start Tor Browser afterwards")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if !fetch {
		fmt.Fprintln(os.Stderr, "usage: oros-system tor --fetch [--dir D] [--no-start]")
		return 2
	}
	o.start = !noStart
	report := progressWriter(o.progress)
	if err := torFetch(o, func(step string, pct int) { report(progress{Step: step, Percent: pct}) }); err != nil {
		report(progress{Step: "error", Done: true, Error: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

type torDownloads struct {
	Version   string `json:"version"`
	Downloads map[string]map[string]struct {
		Binary string `json:"binary"`
		Sig    string `json:"sig"`
	} `json:"downloads"`
}

// torLinks picks the 64-bit Linux build out of downloads.json.
func torLinks(b []byte) (version, binary, sig string, err error) {
	var d torDownloads
	if err := json.Unmarshal(b, &d); err != nil {
		return "", "", "", fmt.Errorf("Tor Browser download list: %v", err)
	}
	for _, key := range []string{"linux-x86_64", "linux64"} {
		if l, ok := d.Downloads[key]["ALL"]; ok && l.Binary != "" && l.Sig != "" {
			for _, u := range []string{l.Binary, l.Sig} {
				if !torURL(u) {
					return "", "", "", fmt.Errorf("unexpected download address %q", u)
				}
			}
			return d.Version, l.Binary, l.Sig, nil
		}
	}
	return "", "", "", errors.New("no Linux build in the Tor Browser download list")
}

// torURL accepts only https addresses on torproject.org.
func torURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "torproject.org" || strings.HasSuffix(h, ".torproject.org")
}

var torHTTP = &http.Client{Timeout: 30 * time.Minute}

func torGet(u string, max int64, out io.Writer, progress func(n, total int64)) error {
	resp, err := torHTTP.Get(u)
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

func torFetch(o torOpts, step func(string, int)) error {
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(o.dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("Tor Browser is already being downloaded")
	}
	if _, err := os.Stat(o.keyring); err != nil {
		return fmt.Errorf("Tor Browser's signing key is missing: %v", err)
	}

	step("index", 0)
	var idx strings.Builder
	if err := torGet(o.index, 1<<20, &idx, nil); err != nil {
		return err
	}
	version, binURL, sigURL, err := torLinks([]byte(idx.String()))
	if err != nil {
		return err
	}
	fmt.Printf("Tor Browser %s: %s\n", version, binURL)

	file := filepath.Join(o.dir, "download.tar.xz")
	sig := file + ".asc"
	defer os.Remove(file)
	defer os.Remove(sig)
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	err = torGet(binURL, torMaxDownload, f, func(n, total int64) {
		if total > 0 {
			step("download", int(n*90/total))
		}
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	s, err := os.Create(sig)
	if err != nil {
		return err
	}
	err = torGet(sigURL, torMaxSig, s, nil)
	if cerr := s.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	step("verify", 92)
	if err := torVerify(o.keyring, sig, file); err != nil {
		return err
	}

	step("unpack", 95)
	tmp := filepath.Join(o.dir, "new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.Command("tar", "-xJf", file, "-C", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking Tor Browser: %v %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(torStartScript(tmp)); err != nil {
		return errors.New("the Tor Browser download has an unexpected layout")
	}
	cur := filepath.Join(o.dir, "tor-browser")
	old := filepath.Join(o.dir, "tor-browser.old")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(filepath.Join(tmp, "tor-browser"), cur); err != nil {
		return err
	}
	_ = os.RemoveAll(old)

	if o.start {
		step("start", 99)
		return startTor(o.dir)
	}
	return nil
}
