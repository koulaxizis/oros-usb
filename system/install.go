package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	progressFile = "/run/oros-install.json"
	liveDir      = "/run/live/medium"
	// The installed disk's persistence partition has its own label, so a
	// stick left in the computer is never mixed up with it.
	diskLabel = "orosdisk"
)

type progress struct {
	Step    string `json:"step"`
	Percent int    `json:"percent"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

type installOpts struct {
	disk, medium, home, progress string
}

// installMain puts orOS on an internal disk the way it runs from the stick:
// a FAT partition with the boot files and the system image (UEFI, Secure
// Boot), and an ext4 partition that keeps the home folder.
func installMain(args []string) int {
	fl := flag.NewFlagSet("install", flag.ContinueOnError)
	var o installOpts
	var yes bool
	fl.StringVar(&o.disk, "disk", "", "target disk (/dev/sdX) or image file; everything on it is erased")
	fl.StringVar(&o.medium, "medium", liveMedium(), "folder with the orOS USB files (the mounted ISO or stick)")
	fl.StringVar(&o.home, "home", "", "home folder to copy onto the disk (optional)")
	fl.StringVar(&o.progress, "progress", progressFile, "progress file (JSON)")
	fl.BoolVar(&yes, "yes", false, "erase the disk without asking")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if o.disk == "" || o.medium == "" {
		fmt.Fprintln(os.Stderr, "need --disk, and --medium when not running from the orOS USB")
		return 2
	}
	// Through sudo (the System button) only the stick and the user's own
	// home folder may be used, and progress goes to its fixed place.
	if u := os.Getenv("SUDO_USER"); u != "" {
		o.medium, o.progress = liveMedium(), progressFile
		if o.home != "" && o.home != "/home/"+u {
			fmt.Fprintln(os.Stderr, "--home must be your own home folder")
			return 2
		}
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "This erases everything on %s. Run again with --yes.\n", o.disk)
		return 2
	}
	last := progress{Percent: -1}
	report := func(p progress) {
		if p.Step == last.Step && p.Percent == last.Percent && !p.Done {
			return
		}
		last = p
		b, _ := json.Marshal(p)
		tmp := o.progress + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, o.progress)
		}
		fmt.Printf("%s %3d%% %s %s\n", time.Now().Format("15:04:05"), p.Percent, p.Step, p.Error)
	}
	if err := install(o, func(step string, pct int) { report(progress{Step: step, Percent: pct}) }); err != nil {
		report(progress{Step: "error", Done: true, Error: err.Error()})
		return 1
	}
	report(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

// liveMedium is the folder of the stick orOS booted from, if any.
func liveMedium() string {
	if _, err := os.Stat(filepath.Join(liveDir, "live", "filesystem.squashfs")); err == nil {
		return liveDir
	}
	return ""
}

// cmdlineFile is read to tell a stick from an installed disk.
var cmdlineFile = "/proc/cmdline"

// installedSystem reports whether orOS runs from a disk it was installed
// on: the installer sets that disk's own persistence label.
func installedSystem() bool {
	b, err := os.ReadFile(cmdlineFile)
	if err != nil {
		return false
	}
	for _, f := range strings.Fields(string(b)) {
		if f == "persistence-label="+diskLabel {
			return true
		}
	}
	return false
}

// bootDisk is the disk that holds the running orOS ("sdb"), never offered
// as a target.
func bootDisk() string {
	src, err := run("findmnt", "-n", "-o", "SOURCE", liveDir)
	if err != nil {
		return ""
	}
	parent, err := run("lsblk", "-n", "-o", "PKNAME", strings.TrimSpace(src))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parent)
}

type disk struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Model string `json:"model"`
	Tran  string `json:"tran"`
}

func installTargets() ([]disk, error) {
	out, err := run("lsblk", "-J", "-b", "-d", "-o", "NAME,SIZE,MODEL,TRAN,TYPE,RO")
	if err != nil {
		return nil, errors.New(strings.TrimSpace(out))
	}
	return pickTargets(out, bootDisk())
}

func pickTargets(lsblkJSON, boot string) ([]disk, error) {
	var l struct {
		Blockdevices []struct {
			Name  string      `json:"name"`
			Size  json.Number `json:"size"`
			Model *string     `json:"model"`
			Tran  *string     `json:"tran"`
			Type  string      `json:"type"`
			RO    any         `json:"ro"`
		} `json:"blockdevices"`
	}
	d := json.NewDecoder(strings.NewReader(lsblkJSON))
	d.UseNumber()
	if err := d.Decode(&l); err != nil {
		return nil, err
	}
	var out []disk
	for _, b := range l.Blockdevices {
		size, _ := b.Size.Int64()
		ro := fmt.Sprint(b.RO)
		if b.Type != "disk" || b.Name == boot || ro == "true" || ro == "1" || size < 8e9 {
			continue
		}
		if strings.HasPrefix(b.Name, "zram") || strings.HasPrefix(b.Name, "loop") {
			continue
		}
		x := disk{Name: b.Name, Size: size}
		if b.Model != nil {
			x.Model = strings.TrimSpace(*b.Model)
		}
		if b.Tran != nil {
			x.Tran = *b.Tran
		}
		out = append(out, x)
	}
	return out, nil
}

func install(o installOpts, step func(string, int)) error {
	if _, err := os.Stat(filepath.Join(o.medium, "live", "filesystem.squashfs")); err != nil {
		return errors.New("the orOS USB files were not found")
	}
	efi := findFold(o.medium, "efi/boot/bootx64.efi")
	if efi == "" {
		return errors.New("the orOS USB has no UEFI boot loader")
	}
	st, err := os.Stat(o.disk)
	if err != nil {
		return err
	}
	isDev := st.Mode()&fs.ModeDevice != 0
	if isDev {
		name := filepath.Base(o.disk)
		if b := bootDisk(); b != "" && b == name {
			return errors.New("this is the stick orOS runs from")
		}
		if mounted(o.disk) {
			return errors.New("a partition of this disk is in use")
		}
	}

	step("prepare", 2)
	total, err := treeSize(o.medium)
	if err != nil {
		return err
	}
	espMiB := int64(4096)
	if need := total/(1<<20) + 1024; need > espMiB {
		espMiB = need
	}
	if out, err := run("wipefs", "-a", o.disk); err != nil {
		return cmdErr("wipefs", out, err)
	}
	table := fmt.Sprintf("label: gpt\nsize=%dMiB, type=U, name=orOS\ntype=L, name=orOS-data\n", espMiB)
	cmd := exec.Command("sfdisk", "--wipe", "always", "--wipe-partitions", "always", o.disk)
	cmd.Stdin = strings.NewReader(table)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdErr("sfdisk", string(out), err)
	}
	parts, err := partitions(o.disk)
	if err != nil {
		return err
	}
	if len(parts) != 2 {
		return fmt.Errorf("expected 2 partitions, found %d", len(parts))
	}
	// Loop devices over each partition work the same on a disk and on an
	// image file, with or without udev.
	var loops []string
	defer func() {
		for _, l := range loops {
			_, _ = run("losetup", "-d", l)
		}
	}()
	for _, p := range parts {
		l, err := run("losetup", "-f", "--show", "-o", strconv.FormatInt(p[0]*512, 10), "--sizelimit", strconv.FormatInt(p[1]*512, 10), o.disk)
		if err != nil {
			return cmdErr("losetup", l, err)
		}
		loops = append(loops, strings.TrimSpace(l))
	}

	step("format", 5)
	if out, err := run("mkfs.vfat", "-F", "32", "-n", "OROS", loops[0]); err != nil {
		return cmdErr("mkfs.vfat", out, err)
	}
	if out, err := run("mkfs.ext4", "-q", "-F", "-L", diskLabel, loops[1]); err != nil {
		return cmdErr("mkfs.ext4", out, err)
	}

	esp, err := mountTmp(loops[0])
	if err != nil {
		return err
	}
	defer unmount(esp)
	var done int64
	err = copyTree(o.medium, esp, func(n int64) {
		done += n
		step("copy", 8+int(77*done/max64(total, 1)))
	})
	if err != nil {
		return err
	}
	if err := patchBootConfigs(esp); err != nil {
		return err
	}

	step("data", 86)
	data, err := mountTmp(loops[1])
	if err != nil {
		return err
	}
	defer unmount(data)
	if err := os.WriteFile(filepath.Join(data, "persistence.conf"), []byte("/home union\n"), 0o644); err != nil {
		return err
	}
	// live-boot keeps the changes to /home in home/rw.
	rw := filepath.Join(data, "home", "rw")
	if err := os.MkdirAll(rw, 0o755); err != nil {
		return err
	}
	// The home folder, and the saved Wi-Fi networks and time zone that live
	// next to it.
	for _, src := range []string{o.home, networkDir, zoneFile} {
		if src == "" {
			continue
		}
		if _, err := os.Stat(src); err == nil {
			if out, err := run("cp", "-a", src, rw+"/"); err != nil {
				return cmdErr("cp", out, err)
			}
		}
	}
	// The browser is running while we copy: its lock files would make the
	// installed system think the profile is open on another computer.
	removeBrowserLocks(rw)

	step("finish", 95)
	_, _ = run("sync")
	unmount(data)
	unmount(esp)
	if isDev {
		if _, err := os.Stat("/sys/firmware/efi"); err == nil {
			rel := strings.TrimPrefix(efi, o.medium)
			_, _ = run("efibootmgr", "-c", "-d", o.disk, "-p", "1", "-L", "orOS", "-l", strings.ReplaceAll(rel, "/", "\\"))
		}
	}
	return nil
}

var bootLive = regexp.MustCompile(`boot=live(\s)`)

// patchBootConfigs makes the installed system look for its own persistence
// partition. Returns an error only when no boot entry could be changed.
func patchBootConfigs(root string) error {
	changed := 0
	for _, dir := range []string{"boot/grub", "isolinux"} {
		files, _ := filepath.Glob(filepath.Join(root, dir, "*.cfg"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			s := string(b)
			if !strings.Contains(s, "boot=live") || strings.Contains(s, "persistence-label=") {
				continue
			}
			s = bootLive.ReplaceAllString(s, "boot=live persistence-label="+diskLabel+"$1")
			if err := os.WriteFile(f, []byte(s), 0o644); err != nil {
				return err
			}
			changed++
		}
	}
	if changed == 0 {
		return errors.New("no boot menu found to update")
	}
	return nil
}

// findFold finds a file by its path in any letter case (FAT and ISO 9660
// differ).
func findFold(root, rel string) string {
	cur := root
	for _, part := range strings.Split(rel, "/") {
		ents, err := os.ReadDir(cur)
		if err != nil {
			return ""
		}
		next := ""
		for _, e := range ents {
			if strings.EqualFold(e.Name(), part) {
				next = filepath.Join(cur, e.Name())
				break
			}
		}
		if next == "" {
			return ""
		}
		cur = next
	}
	return cur
}

func treeSize(root string) (int64, error) {
	var n int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if i, err := d.Info(); err == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n, err
}

// networkDir holds NetworkManager's saved networks (see
// /etc/NetworkManager/conf.d/oros.conf), under /home so they are kept.
var networkDir = "/home/.oros-network"

// removeBrowserLocks deletes Chromium's Singleton* files below root.
func removeBrowserLocks(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "Singleton") {
			_ = os.Remove(p)
		}
		return nil
	})
}

// copyTree copies folders and regular files; links are skipped (the ISO
// has a "debian -> ." loop, and FAT has no links).
func copyTree(src, dst string, progress func(int64)) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			_, err := copyFile(p, target, progress)
			return err
		}
		return nil
	})
}

// countingWriter reports every few MiB, so copying the 1.3 GB system image
// from a slow stick moves the bar instead of looking stuck.
type countingWriter struct {
	w       io.Writer
	pending int64
	report  func(int64)
}

func (p *countingWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.pending += int64(n)
	if p.pending >= 8<<20 {
		p.report(p.pending)
		p.pending = 0
	}
	return n, err
}

func copyFile(src, dst string, progress func(int64)) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	pw := &countingWriter{w: out, report: progress}
	n, err := io.CopyBuffer(pw, in, make([]byte, 1<<20))
	progress(pw.pending)
	if err != nil {
		out.Close()
		return n, err
	}
	return n, out.Close()
}

// partitions returns start and size (512-byte sectors) of each partition.
func partitions(disk string) ([][2]int64, error) {
	out, err := run("partx", "-g", "-o", "START,SECTORS", disk)
	if err != nil {
		return nil, cmdErr("partx", out, err)
	}
	var ps [][2]int64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		a, err1 := strconv.ParseInt(f[0], 10, 64)
		b, err2 := strconv.ParseInt(f[1], 10, 64)
		if err1 == nil && err2 == nil {
			ps = append(ps, [2]int64{a, b})
		}
	}
	return ps, nil
}

func mounted(dev string) bool {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], dev) {
			return true
		}
	}
	return false
}

func mountTmp(dev string) (string, error) {
	dir, err := os.MkdirTemp("", "oros-install-")
	if err != nil {
		return "", err
	}
	if out, err := run("mount", dev, dir); err != nil {
		os.Remove(dir)
		return "", cmdErr("mount", out, err)
	}
	return dir, nil
}

func unmount(dir string) {
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if _, err := run("umount", dir); err == nil {
		os.Remove(dir)
	}
}

func cmdErr(name, out string, err error) error {
	return fmt.Errorf("%s: %s", name, strings.TrimSpace(firstNonEmpty(out, err.Error())))
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
