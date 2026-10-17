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

// The System button's Disks section: the computer's own disks (Windows,
// another Linux) and USB sticks or drives, opened read-only under
// /media/oros so orOS and the Internet window can read them.

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
		if p, err := run("lsblk", "-n", "-o", "PKNAME", f[1]); err == nil {
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
	return pickVolumes(out, systemDisks())
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
		if !volumeFS[fs] || size < 1<<30 || system[b.Name] || system[parent] {
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

// mountVolume mounts v read-only and returns where. Already mounted
// volumes return their place.
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
	opts := "ro,nosuid,nodev,noexec"
	fstype := v.FSType
	switch v.FSType {
	case "vfat", "exfat", "ntfs":
		// No owners on these: show everything as the orOS user's.
		opts += ",uid=" + uid + ",gid=" + gid + ",dmask=022,fmask=133"
		if v.FSType == "ntfs" {
			fstype = "ntfs3"
		}
	case "ext4", "ext3":
		// Read-only even if the journal needs replaying (Linux not shut down).
		opts += ",noload"
	}
	out, err := run("mount", "-t", fstype, "-o", opts, v.Path, dir)
	if err != nil && v.FSType == "ntfs" {
		// The FUSE driver, if the kernel one is missing or refuses.
		out, err = run("mount", "-t", "ntfs-3g", "-o", opts, v.Path, dir)
	}
	if err != nil {
		_ = os.Remove(dir)
		return "", cmdErr("mount", out, err)
	}
	return dir, nil
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

func diskOpen(path string) reply {
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
	r := openWeb("file://" + dir + "/")
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
