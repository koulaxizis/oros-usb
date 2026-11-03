package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPassphrase(t *testing.T) {
	for in, want := range map[string]string{
		"correct horse\n":        "correct horse",
		"Sym-b0ls!@#$\r\nmore\n": "Sym-b0ls!@#$",
		"no newline at all":      "no newline at all",
	} {
		got, err := readPassphrase(strings.NewReader(in))
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
	if _, err := readPassphrase(strings.NewReader("123456789\n")); err == nil {
		t.Fatal("9 characters accepted")
	}
	// The boot screen types with the US layout: a Greek letter could never
	// be typed there.
	if _, err := readPassphrase(strings.NewReader("κωδικός-δέκα\n")); err == nil {
		t.Fatal("Greek letters accepted")
	}
	if _, err := readPassphrase(strings.NewReader("")); err == nil {
		t.Fatal("empty passphrase accepted")
	}
}

func TestLuksArgs(t *testing.T) {
	got := strings.Join(luksArgs("oros-orosdisk", "/dev/loop3", false), " ")
	for _, want := range []string{"luksFormat --batch-mode --type luks2 --label oros-orosdisk", "--pbkdf argon2id", "--pbkdf-memory 262144", "--key-file=- /dev/loop3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q: %s", want, got)
		}
	}
	if got := strings.Join(luksArgs("x", "/dev/y", true), " "); !strings.Contains(got, "--pbkdf argon2id") || !strings.Contains(got, "--pbkdf-force-iterations 4") {
		t.Fatalf("test KDF: %s", got)
	}
	if cryptLabel(diskLabel) != "oros-orosdisk" || cryptLabel("persistence") != "oros-persistence" {
		t.Fatal("labels changed: oros-unlock looks for oros-<persistence label>")
	}
}

func TestPickSysPartition(t *testing.T) {
	out := "/dev/sdb disk\n/dev/sdb1 OROS part\n/dev/sdb3 orossys part\n/dev/sdb4 oros-persistence part\n"
	if p, err := pickSysPartition(out); err != nil || p != "/dev/sdb3" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := pickSysPartition("/dev/sdb1 OROS part\n"); err == nil {
		t.Fatal("found an orossys partition that is not there")
	}
	// lsblk without udev: no labels, blkid gives them
	old := partLabel
	defer func() { partLabel = old }()
	partLabel = func(dev string) string {
		if dev == "/dev/loop0p3" {
			return "orossys"
		}
		return ""
	}
	if p, err := pickSysPartition("/dev/loop0  disk\n/dev/loop0p1  part\n/dev/loop0p3  part\n"); err != nil || p != "/dev/loop0p3" {
		t.Fatalf("blkid fallback: %q %v", p, err)
	}
}

// With encrypted data the slots go on the orossys partition, never into
// the encrypted container, and the boot menu points at orossys.
func TestUpdateEncrypted(t *testing.T) {
	oldUUID, oldSrc, oldSys, oldMount := partitionUUID, mountSource, sysPartition, mountSys
	defer func() { partitionUUID, mountSource, sysPartition, mountSys = oldUUID, oldSrc, oldSys, oldMount }()
	src, data, sys := t.TempDir(), t.TempDir(), t.TempDir()
	released := false
	mountSource = func(dir string) string {
		if dir == data {
			return "/dev/mapper/oros-data"
		}
		return "/dev/sdb3"
	}
	sysPartition = func(dev string) (string, error) {
		if dev != "/dev/mapper/oros-data" {
			t.Fatalf("asked for %q", dev)
		}
		return "/dev/sdb3", nil
	}
	mountSys = func(dev string) (string, func(), error) { return sys, func() { released = true }, nil }
	partitionUUID = func(dir string) (string, error) {
		if dir != sys {
			t.Fatalf("UUID of %q, not of orossys", dir)
		}
		return "sys-1", nil
	}
	writeRelease(t, src, "2026.11.01", false)
	cmd := "BOOT_IMAGE=/live/vmlinuz boot=live components quiet persistence persistence-label=orosdisk"
	if err := update(updateOpts{source: src, target: data, cmdline: cmd}, func(string, int) {}); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("orossys left mounted")
	}
	if _, err := os.Stat(filepath.Join(data, "oros-system")); err == nil {
		t.Fatal("slots written into the encrypted data")
	}
	cfg, err := os.ReadFile(filepath.Join(sys, "oros-system", "boot.cfg"))
	if err != nil || !strings.Contains(string(cfg), "persistence-label=orosdisk live-media=/dev/disk/by-uuid/sys-1 live-media-path=/oros-system/A toram=filesystem.squashfs") {
		t.Fatalf("boot.cfg %v:\n%s", err, cfg)
	}
}

// The parents of the encrypted data come from sysfs, as on a real
// computer: /sys/class/block/<name> links into the device tree.
func TestDMParents(t *testing.T) {
	root := t.TempDir()
	devs := filepath.Join(root, "devices")
	for _, d := range []string{"pci/nvme0n1/nvme0n1p3", "virtual/dm-0/slaves", "pci/nvme0n1/nvme0n1p2"} {
		if err := os.MkdirAll(filepath.Join(devs, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(devs, "pci/nvme0n1/nvme0n1p3/partition"), []byte("3\n"), 0o644)
	class := filepath.Join(root, "class")
	os.MkdirAll(class, 0o755)
	for name, to := range map[string]string{"nvme0n1p3": "pci/nvme0n1/nvme0n1p3", "dm-0": "virtual/dm-0", "nvme0n1": "pci/nvme0n1"} {
		if err := os.Symlink(filepath.Join(devs, to), filepath.Join(class, name)); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink(filepath.Join(class, "nvme0n1p3"), filepath.Join(devs, "virtual/dm-0/slaves/nvme0n1p3"))
	// /dev/mapper/oros-orosdisk is a link to ../dm-0
	dev := filepath.Join(root, "dev")
	os.MkdirAll(filepath.Join(dev, "mapper"), 0o755)
	os.WriteFile(filepath.Join(dev, "dm-0"), nil, 0o644)
	os.Symlink("../dm-0", filepath.Join(dev, "mapper", "oros-orosdisk"))
	part, disk, err := dmParents(class, filepath.Join(dev, "mapper", "oros-orosdisk"))
	if err != nil || part != "nvme0n1p3" || disk != "nvme0n1" {
		t.Fatalf("got %q %q %v", part, disk, err)
	}
	// without udev /dev/mapper/<name> is a plain node: matched by dm/name
	os.MkdirAll(filepath.Join(devs, "virtual/dm-0/dm"), 0o755)
	os.WriteFile(filepath.Join(devs, "virtual/dm-0/dm/name"), []byte("oros-test-disk\n"), 0o644)
	os.WriteFile(filepath.Join(dev, "mapper", "oros-test-disk"), nil, 0o644)
	part, disk, err = dmParents(class, filepath.Join(dev, "mapper", "oros-test-disk"))
	if err != nil || part != "nvme0n1p3" || disk != "nvme0n1" {
		t.Fatalf("device node: got %q %q %v", part, disk, err)
	}
	if _, _, err := dmParents(class, "/dev/mapper/none"); err == nil {
		t.Error("a device with no slaves passed")
	}
}
