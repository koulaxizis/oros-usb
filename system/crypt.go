package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// Encrypted data (optional). A stick or an installed disk made with
// encryption keeps the data partition in a LUKS2 container labelled
// "oros-<persistence label>" ("oros-persistence" on a stick, made by
// scripts/add-persistence.sh --encrypt or make-combo-sticks.sh --encrypt;
// "oros-orosdisk" on a disk, made by "oros-system install --encrypt"). The
// initramfs script oros-unlock asks for the passphrase on the boot screen
// and opens it as /dev/mapper/oros-data; live-boot then finds the ext4
// inside by its usual label.
//
// GRUB reads system updates (slots A/B and boot.cfg) before the passphrase
// is asked, so with encryption they live on their own unencrypted ext4
// partition labelled "orossys" on the same disk. It holds only the public
// orOS system, never personal data.

const (
	sysLabel      = "orossys"
	sysMiB        = 5120
	minPassphrase = 10
)

// cryptLabel is the LUKS2 label oros-unlock looks for.
func cryptLabel(persistenceLabel string) string { return "oros-" + persistenceLabel }

// readPassphrase reads the passphrase from the first line of r (the System
// button passes it through a pipe, never on the command line).
func readPassphrase(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 4096)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	p := strings.TrimRight(line, "\r\n")
	return p, checkPassphrase(p)
}

// checkPassphrase: at least 10 characters, and only what the boot screen
// can type. At that point the keyboard has the US English layout whatever
// the computer's own keyboard is, so a Greek letter could never be typed
// and the data would stay locked for good.
func checkPassphrase(p string) error {
	if utf8.RuneCountInString(p) < minPassphrase {
		return fmt.Errorf("the passphrase needs at least %d characters", minPassphrase)
	}
	for _, c := range p {
		if c < 0x20 || c > 0x7e {
			return errors.New("the passphrase may hold only Latin letters, digits, spaces and the symbols on a US keyboard")
		}
	}
	return nil
}

// luksArgs are the cryptsetup options for a new data container: LUKS2,
// Argon2id with 256 MiB, about 2 s on the computer that makes it. With
// OROS_LUKS_TEST=1 (CI only; sudo drops it) Argon2id stays but is fast, as
// the test VM may run without hardware acceleration. Same settings as
// scripts/encrypted-data.sh.
func luksArgs(label, dev string, test bool) []string {
	kdf := []string{"--pbkdf", "argon2id", "--pbkdf-memory", "262144", "--pbkdf-parallel", "4", "--iter-time", "2000"}
	if test {
		kdf = []string{"--pbkdf", "argon2id", "--pbkdf-memory", "32768", "--pbkdf-parallel", "1", "--pbkdf-force-iterations", "4"}
	}
	args := []string{"luksFormat", "--batch-mode", "--type", "luks2", "--label", label}
	args = append(args, kdf...)
	return append(args, "--key-file=-", dev)
}

// runInput runs a command with the given standard input.
var runInput = func(input, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// makeEncrypted formats dev as a LUKS2 container with the passphrase and
// opens it as /dev/mapper/<name>. The caller closes it with closeEncrypted.
func makeEncrypted(dev, label, name, pass string) (string, error) {
	test := os.Getenv("OROS_LUKS_TEST") == "1"
	if out, err := runInput(pass, "cryptsetup", luksArgs(label, dev, test)...); err != nil {
		return "", cmdErr("cryptsetup luksFormat", out, err)
	}
	_ = os.MkdirAll("/run/cryptsetup", 0o700)
	if out, err := runInput(pass, "cryptsetup", "open", "--type", "luks2", "--key-file=-", dev, name); err != nil {
		return "", cmdErr("cryptsetup open", out, err)
	}
	return "/dev/mapper/" + name, nil
}

func closeEncrypted(name string) {
	_, _ = run("cryptsetup", "close", name)
}

// mountSource is the device mounted at dir.
var mountSource = func(dir string) string {
	out, err := run("findmnt", "-n", "-o", "SOURCE", "--target", dir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func isMapper(dev string) bool {
	return strings.HasPrefix(dev, "/dev/mapper/") || strings.HasPrefix(dev, "/dev/dm-")
}

// sysPartition finds the orossys partition on the disk that holds the
// encrypted data device dev (/dev/mapper/oros-data).
var sysPartition = func(dev string) (string, error) {
	part, err := run("lsblk", "-dnro", "PKNAME", dev)
	part = strings.TrimSpace(part)
	if err != nil || part == "" {
		return "", errors.New("the encrypted data partition was not found")
	}
	disk, err := run("lsblk", "-dnro", "PKNAME", "/dev/"+part)
	disk = strings.TrimSpace(disk)
	if err != nil || disk == "" {
		return "", errors.New("the disk of the encrypted data was not found")
	}
	out, err := run("lsblk", "-nro", "PATH,LABEL,TYPE", "/dev/"+disk)
	if err != nil {
		return "", cmdErr("lsblk", out, err)
	}
	return pickSysPartition(out)
}

func pickSysPartition(lsblk string) (string, error) {
	for _, line := range strings.Split(lsblk, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[1] == sysLabel && f[2] == "part" {
			return f[0], nil
		}
	}
	return "", errors.New("no " + sysLabel + " partition for system updates next to the encrypted data")
}

// mountSys mounts the orossys partition for writing, where it is not
// mounted yet (after a start from a slot, toram released it).
var mountSys = func(dev string) (string, func(), error) {
	if out, err := run("findmnt", "-n", "-o", "TARGET,OPTIONS", "--source", dev); err == nil {
		if f := strings.Fields(out); len(f) >= 2 && strings.HasPrefix(f[1], "rw") {
			return f[0], func() {}, nil
		}
		return "", nil, errors.New(dev + " is in use read-only")
	}
	dir, err := mountTmp(dev)
	if err != nil {
		return "", nil, err
	}
	return dir, func() { unmount(dir) }, nil
}

// slotsDir is where system updates go: the persistence partition itself,
// or the orossys partition when the data is encrypted.
func slotsDir(target string) (string, func(), error) {
	src := mountSource(target)
	if !isMapper(src) {
		return target, func() {}, nil
	}
	dev, err := sysPartition(src)
	if err != nil {
		return "", nil, err
	}
	return mountSys(dev)
}
