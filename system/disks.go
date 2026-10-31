package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

// The System button's Disks section and orOS Files' "This computer": the
// computer's own disks (Windows, another Linux) and USB sticks or drives,
// opened under /media/oros (Windows disks read-only, see mountVolume).

var mediaRoot = "/media/oros"

// volume is one partition (or unpartitioned stick) that can be opened.
type volume struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	FSType   string `json:"fstype"`
	Size     int64  `json:"size"`
	Model    string `json:"model"`
	USB      bool   `json:"usb"`
	Mount    string `json:"mount,omitempty"`
	ReadOnly bool   `json:"readonly,omitempty"` // mounted read-only
	Internal bool   `json:"internal"`
}

// Filesystems Debian's kernel reads without extra packages.
var volumeFS = map[string]bool{"vfat": true, "exfat": true, "ntfs": true, "ext4": true, "ext3": true, "ext2": true, "btrfs": true, "xfs": true}

// lsblkRun lists block devices; a variable so tests can feed their own.
var lsblkRun = func() (string, error) {
	out, err := run("lsblk", "-J", "-l", "-b", "-o", "NAME,PATH,PKNAME,TYPE,SIZE,FSTYPE,LABEL,MOUNTPOINT,RM,HOTPLUG,TRAN,MODEL")
	if err != nil {
		return "", errors.New(strings.TrimSpace(out))
	}
	return out, nil
}

// systemDisks are the disks orOS itself runs from: the stick or the
// installed disk (the medium and the persistence partition).
var systemDisks = func() map[string]bool {
	set := map[string]bool{}
	out, _ := run("findmnt", "-rn", "-o", "TARGET,SOURCE")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || !strings.HasPrefix(f[0], "/run/live") || !strings.HasPrefix(f[1], "/dev/") {
			continue
		}
		set[strings.TrimPrefix(f[1], "/dev/")] = true
		if p, err := run("lsblk", "-n", "-o", "PKNAME", f[1]); err == nil && strings.TrimSpace(p) != "" {
			set[strings.TrimSpace(p)] = true
		}
	}
	if b := bootDisk(); b != "" {
		set[b] = true
	}
	return set
}

func listVolumes() ([]volume, error) {
	out, err := lsblkRun()
	if err != nil {
		return nil, err
	}
	vs, err := pickVolumes(out, systemDisks())
	if err != nil {
		return nil, err
	}
	ro := readOnlyMounts()
	for i := range vs {
		vs[i].ReadOnly = vs[i].Mount != "" && ro[vs[i].Mount]
	}
	return vs, nil
}

// disksMain is "oros-system disks": the Disks list as JSON, for the orOS
// launcher's computer files (Files' "This computer"). No root needed.
func disksMain() int {
	vs, err := listVolumes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	if vs == nil {
		vs = []volume{}
	}
	b, _ := json.Marshal(vs)
	fmt.Println(string(b))
	return 0
}

// pickVolumes keeps partitions with a readable filesystem, at least 1 GiB
// (boot and recovery partitions are smaller), off the disks orOS runs from.
func pickVolumes(lsblkJSON string, system map[string]bool) ([]volume, error) {
	var l struct {
		Blockdevices []struct {
			Name, Path, Type string
			PKName           *string `json:"pkname"`
			Size             json.Number
			FSType           *string `json:"fstype"`
			Label            *string
			Mountpoint       *string
			RM, Hotplug      any
			Tran, Model      *string
		} `json:"blockdevices"`
	}
	d := json.NewDecoder(strings.NewReader(lsblkJSON))
	d.UseNumber()
	if err := d.Decode(&l); err != nil {
		return nil, err
	}
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return strings.TrimSpace(*s)
	}
	yes := func(v any) bool { s := fmt.Sprint(v); return s == "true" || s == "1" }
	type diskInfo struct {
		model string
		usb   bool
	}
	disks := map[string]diskInfo{}
	for _, b := range l.Blockdevices {
		if b.Type == "disk" {
			disks[b.Name] = diskInfo{str(b.Model), str(b.Tran) == "usb" || yes(b.RM) || yes(b.Hotplug)}
		}
	}
	var out []volume
	for _, b := range l.Blockdevices {
		parent := str(b.PKName)
		if b.Type != "part" && b.Type != "disk" {
			continue
		}
		fs := str(b.FSType)
		size, _ := b.Size.Int64()
		label := str(b.Label)
		// A whole disk (a stick formatted without partitions) has no
		// parent: "" never names a system disk.
		if !volumeFS[fs] || size < 1<<30 || system[b.Name] || (parent != "" && system[parent]) {
			continue
		}
		if label == "persistence" || label == diskLabel {
			continue
		}
		if strings.HasPrefix(b.Name, "zram") || strings.HasPrefix(b.Name, "loop") {
			continue
		}
		info := disks[b.Name]
		if b.Type == "part" {
			info = disks[parent]
		}
		v := volume{Path: b.Path, Label: label, FSType: fs, Size: size, Model: info.model, USB: info.usb, Internal: !info.usb}
		if mp := str(b.Mountpoint); strings.HasPrefix(mp, mediaRoot+"/") {
			v.Mount = mp
		}
		out = append(out, v)
	}
	return out, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// mountDir is where a volume opens: its label, or its device name.
func mountDir(v volume) string {
	name := strings.TrimSpace(unsafeName.ReplaceAllString(v.Label, "_"))
	if name == "" || name == "." || name == ".." {
		name = filepath.Base(v.Path)
	}
	return filepath.Join(mediaRoot, name)
}

func findVolume(path string) (volume, error) {
	vs, err := listVolumes()
	if err != nil {
		return volume{}, err
	}
	for _, v := range vs {
		if v.Path == path {
			return v, nil
		}
	}
	return volume{}, errors.New("this disk cannot be opened")
}

// mountVolume mounts v and returns where. USB sticks and drives and the
// computer's other Linux disks open writable; Windows disks (NTFS inside
// the computer, or any NTFS that holds Windows) open read-only, because
// writing while Windows hibernates (Fast Startup) can damage them. A disk
// that refuses to open writable (not shut down cleanly) opens read-only.
// Already mounted volumes return their place.
func mountVolume(v volume, uid, gid string) (string, error) {
	if v.Mount != "" {
		return v.Mount, nil
	}
	dir := mountDir(v)
	if isMountpoint(dir) {
		dir = filepath.Join(mediaRoot, filepath.Base(v.Path))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if v.FSType == "ntfs" {
		// Read-only first, to look for Windows before writing anything.
		out, err := mountAs(v, dir, uid, gid, false)
		if err != nil {
			_ = os.Remove(dir)
			return "", cmdErr("mount", out, err)
		}
		if !v.USB || holdsWindows(dir) {
			return dir, nil
		}
		if out, err := run("umount", dir); err != nil {
			return "", cmdErr("umount", out, err)
		}
	}
	out, err := mountAs(v, dir, uid, gid, true)
	if err != nil {
		out, err = mountAs(v, dir, uid, gid, false)
	}
	if err != nil {
		_ = os.Remove(dir)
		return "", cmdErr("mount", out, err)
	}
	return dir, nil
}

func mountAs(v volume, dir, uid, gid string, rw bool) (string, error) {
	opts := "ro,nosuid,nodev,noexec"
	if rw {
		opts = "rw,nosuid,nodev,noexec"
	}
	fstype := v.FSType
	switch v.FSType {
	case "vfat", "exfat", "ntfs":
		// No owners on these: show everything as the orOS user's.
		opts += ",uid=" + uid + ",gid=" + gid + ",dmask=022,fmask=133"
		if v.FSType == "ntfs" {
			fstype = "ntfs3"
		}
	case "ext4", "ext3":
		if !rw {
			// Read-only even if the journal needs replaying (Linux not shut down).
			opts += ",noload"
		}
	}
	out, err := run("mount", "-t", fstype, "-o", opts, v.Path, dir)
	if err != nil && v.FSType == "ntfs" {
		// The FUSE driver, if the kernel one is missing or refuses.
		out, err = run("mount", "-t", "ntfs-3g", "-o", opts, v.Path, dir)
	}
	return out, err
}

// holdsWindows tells a Windows system disk by its files.
func holdsWindows(dir string) bool {
	for _, n := range []string{"Windows/System32", "WINDOWS/system32", "windows/system32", "hiberfil.sys", "pagefile.sys", "swapfile.sys"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// readOnlyMounts are the mount points mounted read-only now.
func readOnlyMounts() map[string]bool {
	set := map[string]bool{}
	b, _ := os.ReadFile("/proc/mounts")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 3 && (f[3] == "ro" || strings.HasPrefix(f[3], "ro,")) {
			set[f[1]] = true
		}
	}
	return set
}

func isMountpoint(dir string) bool {
	b, _ := os.ReadFile("/proc/mounts")
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[1] == dir {
			return true
		}
	}
	return false
}

func unmountVolume(v volume) error {
	if v.Mount == "" {
		return nil
	}
	if out, err := run("umount", v.Mount); err != nil {
		return cmdErr("umount", out, err)
	}
	_ = os.Remove(v.Mount)
	return nil
}

// mountMain is "oros-system mount --dev /dev/sdX1 [--unmount]", run as
// root through sudo. It only touches volumes the Disks section lists.
func mountMain(args []string) int {
	fl := flag.NewFlagSet("mount", flag.ContinueOnError)
	dev := fl.String("dev", "", "partition to open")
	off := fl.Bool("unmount", false, "close it instead")
	if err := fl.Parse(args); err != nil || *dev == "" {
		fmt.Fprintln(os.Stderr, "usage: oros-system mount --dev /dev/sdX1 [--unmount]")
		return 2
	}
	v, err := findVolume(*dev)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	if *off {
		if err := unmountVolume(v); err != nil {
			fmt.Fprintln(os.Stderr, "Error: "+err.Error())
			return 1
		}
		return 0
	}
	uid, gid := "0", "0"
	name := os.Getenv("SUDO_USER")
	if name == "" {
		name = "oros"
	}
	if u, err := user.Lookup(name); err == nil {
		uid, gid = u.Uid, u.Gid
	}
	dir, err := mountVolume(v, uid, gid)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	fmt.Println(dir)
	return 0
}

// The host side: list, open (mount, then show in the Internet window),
// close.

func downloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func disksList() reply {
	vs, err := listVolumes()
	if err != nil {
		return fail(err, "")
	}
	if vs == nil {
		vs = []volume{}
	}
	return reply{"ok": true, "volumes": vs}
}

// diskMount opens a disk (or Downloads) and says where, without showing
// it: orOS Files shows it when orOS has "This computer".
func diskMount(path string) reply {
	dir := downloadsDir()
	if path != "downloads" {
		out, err := rootRun("mount", "--dev", path)
		if err != nil {
			return fail(err, out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		dir = lines[len(lines)-1]
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err, "")
	}
	return reply{"ok": true, "mount": dir}
}

// diskOpen opens a disk and shows it in the Internet window (orOS
// versions without "This computer" in Files).
func diskOpen(path string) reply {
	r := diskMount(path)
	if r["ok"] != true {
		return r
	}
	dir := r["mount"].(string)
	r = openWeb("file://" + dir + "/")
	r["mount"] = dir
	return r
}

func diskClose(path string) reply {
	out, err := rootRun("mount", "--dev", path, "--unmount")
	if err != nil {
		return fail(err, out)
	}
	return reply{"ok": true}
}

// rootRun runs oros-system as root through the narrow sudo rule and waits.
var rootRun = func(args ...string) (string, error) {
	return run("sudo", append([]string{"-n", "/opt/oros/oros-system"}, args...)...)
}
