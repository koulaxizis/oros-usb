package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSuperblockEncrypt(t *testing.T) {
	sb := make([]byte, 1024)
	if _, err := superblockHasEncrypt(sb); err == nil {
		t.Fatal("no magic accepted")
	}
	binary.LittleEndian.PutUint16(sb[sbMagicOffset:], ext4Magic)
	if ok, _ := superblockHasEncrypt(sb); ok {
		t.Fatal("encrypt seen without the bit")
	}
	binary.LittleEndian.PutUint32(sb[sbIncompat:], incompatCrypt|0x2)
	if ok, _ := superblockHasEncrypt(sb); !ok {
		t.Fatal("encrypt bit missed")
	}
	// A real file system, when mkfs.ext4 is there.
	if _, err := exec.LookPath("mkfs.ext4"); err != nil {
		t.Skip("no mkfs.ext4")
	}
	for _, c := range []struct {
		opts []string
		want bool
	}{{nil, false}, {[]string{"-O", "encrypt"}, true}} {
		img := filepath.Join(t.TempDir(), "fs.img")
		must(t, os.WriteFile(img, nil, 0o600))
		must(t, os.Truncate(img, 16<<20))
		args := append([]string{"-q", "-F"}, c.opts...)
		if out, err := exec.Command("mkfs.ext4", append(args, img)...).CombinedOutput(); err != nil {
			t.Fatalf("mkfs: %v %s", err, out)
		}
		if ok, err := ext4HasEncrypt(img); err != nil || ok != c.want {
			t.Fatalf("%v: %v %v", c.opts, ok, err)
		}
	}
}

func TestVaultBase(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("chown needs root")
	}
	target := t.TempDir()
	oldM, oldC := vaultMount, ext4Check
	defer func() { vaultMount, ext4Check = oldM, oldC }()
	vaultMount = func(string) (string, string) { return "/dev/sdz3", "ext4" }
	enc := false
	ext4Check = func(string) (bool, error) { return enc, nil }
	if _, err := vaultBase("", "1000", "1000"); err == nil {
		t.Fatal("no persistence accepted")
	}
	if _, err := vaultBase(target, "", ""); err == nil {
		t.Fatal("not through sudo accepted")
	}
	if _, err := vaultBase(target, "0", "0"); err == nil {
		t.Fatal("root accepted")
	}
	if _, err := vaultBase(target, "1000", "1000"); err == nil {
		t.Fatal("no encrypt feature accepted")
	}
	enc = true
	vaultMount = func(string) (string, string) { return "/dev/sdz3", "ext3" }
	if _, err := vaultBase(target, "1000", "1000"); err == nil {
		t.Fatal("ext3 accepted")
	}
	vaultMount = func(string) (string, string) { return "/dev/mapper/oros-data", "ext4" }
	must(t, os.Mkdir(filepath.Join(target, vaultFolder), 0o777))
	base, err := vaultBase(target, "1000", "1001")
	if err != nil || base != filepath.Join(target, vaultFolder) {
		t.Fatalf("base %q %v", base, err)
	}
	fi, _ := os.Lstat(base)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// A link planted there is refused.
	must(t, os.RemoveAll(base))
	must(t, os.Symlink("/etc", base))
	if _, err := vaultBase(target, "1000", "1000"); err == nil {
		t.Fatal("link accepted")
	}
}
