package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// System updates: the monthly orOS USB release carries the system image
// (filesystem.squashfs, vmlinuz, initrd.img) and update.json. "oros-system
// update" downloads them onto the persistence partition, into slot A or B
// (whichever is not running), and writes /oros-system/boot.cfg there. The
// image's GRUB menu (UEFI) loads that file first: the new system starts
// by default, the previous one and the system on the stick stay in the
// menu. The persistence partition only holds /home, so a new system keeps
// every file and setting.

const (
	updateProgressFile = "/run/oros-update.json"
	updateDir          = "oros-system"
	updateURL          = "https://github.com/koulaxizis/oros-usb/releases/latest/download/update.json"
	versionFile        = "/opt/oros/version"
	persistenceRoot    = "/run/live/persistence"
)

var updateFiles = []string{"filesystem.squashfs", "vmlinuz", "initrd.img"}

type updateManifest struct {
	Version string       `json:"version"`
	Files   []updateFile `json:"files"`
}

type updateFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (m updateManifest) size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

func (m updateManifest) check() error {
	if !regexp.MustCompile(`^[0-9A-Za-z.+-]{1,40}$`).MatchString(m.Version) {
		return fmt.Errorf("bad version %q", m.Version)
	}
	have := map[string]bool{}
	for _, f := range m.Files {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(f.SHA256) || f.Size <= 0 {
			return fmt.Errorf("bad entry for %q", f.Name)
		}
		have[f.Name] = true
	}
	for _, n := range updateFiles {
		if !have[n] {
			return fmt.Errorf("update.json lacks %s", n)
		}
	}
	return nil
}

func currentVersion() string {
	b, _ := os.ReadFile(versionFile)
	return strings.TrimSpace(string(b))
}

// fetchManifest reads update.json from a URL or a folder.
func fetchManifest(src string) (updateManifest, error) {
	var m updateManifest
	var r io.ReadCloser
	if strings.HasPrefix(src, "https://") {
		c := &http.Client{Timeout: 30 * time.Second}
		res, err := c.Get(src)
		if err != nil {
			return m, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return m, fmt.Errorf("update.json: %s", res.Status)
		}
		r = res.Body
	} else {
		f, err := os.Open(filepath.Join(src, "update.json"))
		if err != nil {
			return m, err
		}
		r = f
	}
	defer r.Close()
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&m); err != nil {
		return m, err
	}
	return m, m.check()
}

// openSource opens one file of the release next to update.json.
func openSource(src, name string) (io.ReadCloser, error) {
	if !strings.HasPrefix(src, "https://") {
		return os.Open(filepath.Join(src, name))
	}
	u, err := url.Parse(src)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(path.Dir(u.Path), name)
	res, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("%s: %s", name, res.Status)
	}
	return res.Body, nil
}

// persistenceTarget is the mounted persistence partition of the running
// system (the stick's "persistence" or the installed disk's "orosdisk").
func persistenceTarget() string {
	dirs, _ := filepath.Glob(filepath.Join(persistenceRoot, "*"))
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "persistence.conf")); err == nil {
			return d
		}
	}
	return ""
}

var mediaPathRe = regexp.MustCompile(`(?:^|\s)live-media-path=/` + updateDir + `/([AB])(?:\s|$)`)

// runningSlot is the slot the running system started from, "" for the
// system on the stick itself.
func runningSlot(cmdline string) string {
	if m := mediaPathRe.FindStringSubmatch(cmdline); m != nil {
		return m[1]
	}
	return ""
}

// bootArgs keeps the running kernel's live options and points the live
// system at a slot on the partition with the given UUID.
func bootArgs(cmdline, uuid, slot string) string {
	var keep []string
	for _, a := range strings.Fields(cmdline) {
		k := strings.SplitN(a, "=", 2)[0]
		switch k {
		case "BOOT_IMAGE", "initrd", "findiso", "live-media", "live-media-path", "live-media-timeout":
			continue
		}
		keep = append(keep, a)
	}
	keep = append(keep, "live-media=/dev/disk/by-uuid/"+uuid, "live-media-path=/"+updateDir+"/"+slot)
	return strings.Join(keep, " ")
}

// partitionUUID is the file system UUID of the partition mounted at dir.
var partitionUUID = func(dir string) (string, error) {
	src, err := run("findmnt", "-n", "-o", "SOURCE", "--target", dir)
	if err != nil {
		return "", cmdErr("findmnt", src, err)
	}
	uuid, err := run("blkid", "-s", "UUID", "-o", "value", strings.TrimSpace(src))
	uuid = strings.TrimSpace(uuid)
	if err != nil || uuid == "" {
		return "", cmdErr("blkid", uuid, errors.New("no UUID"))
	}
	return uuid, nil
}

type bootEntry struct{ title, slot, args string }

// bootConfig is the GRUB file the image's menu loads from the persistence
// partition ($orosupd is that partition).
func bootConfig(entries []bootEntry) string {
	var b strings.Builder
	b.WriteString("# Written by oros-system update. The orOS USB menu loads this file.\nset default=0\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "menuentry %q {\n\tlinux ($orosupd)/%s/%s/vmlinuz %s\n\tinitrd ($orosupd)/%s/%s/initrd.img\n}\n",
			e.title, updateDir, e.slot, e.args, updateDir, e.slot)
	}
	return b.String()
}

type updateOpts struct {
	source, target, progress, cmdline string
}

func updateMain(args []string) int {
	fl := flag.NewFlagSet("update", flag.ContinueOnError)
	var o updateOpts
	var yes bool
	fl.StringVar(&o.source, "source", updateURL, "update.json URL, or a folder with update.json and the files")
	fl.StringVar(&o.target, "target", "", "mounted persistence partition (default: the running one)")
	fl.StringVar(&o.progress, "progress", updateProgressFile, "progress file (JSON)")
	fl.StringVar(&o.cmdline, "cmdline", "", "kernel options to start the new system with (default: the running ones)")
	fl.BoolVar(&yes, "yes", false, "update without asking")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	// Through sudo (the System button) only the official release, the
	// running system's partition and the fixed progress file are used.
	if os.Getenv("SUDO_USER") != "" {
		o = updateOpts{source: updateURL, progress: updateProgressFile}
	}
	if !yes {
		fmt.Fprintln(os.Stderr, "Run again with --yes to download and install the new system.")
		return 2
	}
	report := progressWriter(o.progress)
	if err := update(o, func(step string, pct int) { report(progress{Step: step, Percent: pct}) }); err != nil {
		report(progress{Step: "error", Done: true, Error: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

func progressWriter(file string) func(progress) {
	last := progress{Percent: -1}
	return func(p progress) {
		if p.Step == last.Step && p.Percent == last.Percent && !p.Done {
			return
		}
		last = p
		b, _ := json.Marshal(p)
		tmp := file + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, file)
		}
		fmt.Printf("%3d%% %s %s\n", p.Percent, p.Step, p.Error)
	}
}

func update(o updateOpts, step func(string, int)) error {
	if o.target == "" {
		o.target = persistenceTarget()
	}
	if o.target == "" {
		return errors.New("no persistence partition: updates need one to keep the new system")
	}
	cmdline := o.cmdline
	if cmdline == "" {
		b, err := os.ReadFile("/proc/cmdline")
		if err != nil {
			return err
		}
		cmdline = string(b)
	}
	if !strings.Contains(" "+cmdline+" ", " boot=live ") {
		return errors.New("not a live orOS system")
	}
	uuid, err := partitionUUID(o.target)
	if err != nil {
		return err
	}

	step("check", 0)
	m, err := fetchManifest(o.source)
	if err != nil {
		return err
	}
	base := filepath.Join(o.target, updateDir)
	cur := runningSlot(cmdline)
	slot := "A"
	if cur == "A" {
		slot = "B"
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	// The slot being replaced and leftovers of a broken download are freed
	// first; the running slot stays.
	newDir := filepath.Join(base, slot+".new")
	os.RemoveAll(newDir)
	os.RemoveAll(filepath.Join(base, slot))
	var st syscall.Statfs_t
	if err := syscall.Statfs(o.target, &st); err != nil {
		return err
	}
	if free := int64(st.Bavail) * int64(st.Bsize); free < m.size()+256<<20 {
		return fmt.Errorf("not enough space: %d MB free, %d MB needed", free>>20, (m.size()+256<<20)>>20)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return err
	}
	var done int64
	total := m.size()
	for _, f := range m.Files {
		if err := fetchFile(o.source, f, newDir, func(n int64) {
			step("download", int((done+n)*95/total))
		}); err != nil {
			os.RemoveAll(newDir)
			return err
		}
		done += f.Size
	}
	if err := os.WriteFile(filepath.Join(newDir, "version"), []byte(m.Version+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(newDir, filepath.Join(base, slot)); err != nil {
		return err
	}

	step("boot", 97)
	entries := []bootEntry{{"orOS " + m.Version, slot, bootArgs(cmdline, uuid, slot)}}
	if cur != "" {
		if v, err := os.ReadFile(filepath.Join(base, cur, "version")); err == nil {
			entries = append(entries, bootEntry{"orOS " + strings.TrimSpace(string(v)) + " (previous)", cur, bootArgs(cmdline, uuid, cur)})
		}
	}
	cfg := filepath.Join(base, "boot.cfg")
	if err := os.WriteFile(cfg+".tmp", []byte(bootConfig(entries)), 0o644); err != nil {
		return err
	}
	if err := os.Rename(cfg+".tmp", cfg); err != nil {
		return err
	}
	syscall.Sync()
	return nil
}

// fetchFile copies one release file into dir and checks its size and
// SHA-256 before it counts.
func fetchFile(src string, f updateFile, dir string, progress func(int64)) error {
	if filepath.Base(f.Name) != f.Name {
		return fmt.Errorf("bad file name %q", f.Name)
	}
	in, err := openSource(src, f.Name)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(dir, f.Name))
	if err != nil {
		return err
	}
	h := sha256.New()
	var n int64
	buf := make([]byte, 1<<20)
	for {
		k, rerr := in.Read(buf)
		if k > 0 {
			if _, err := out.Write(buf[:k]); err != nil {
				out.Close()
				return err
			}
			h.Write(buf[:k])
			n += int64(k)
			if n > f.Size {
				out.Close()
				return fmt.Errorf("%s: larger than expected", f.Name)
			}
			progress(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return fmt.Errorf("%s: %v", f.Name, rerr)
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%s: download damaged (checksum)", f.Name)
	}
	return nil
}
