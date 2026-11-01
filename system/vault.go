package main

// Encrypted users (oros-portable launcher/vault.go, U4). Each user who
// turns encryption on gets an fscrypt folder on the data partition,
// outside the /home overlay (fscrypt does not work through overlayfs).
//
//   - "oros-system vault-base" (root, through sudo, for the user oros):
//     makes <persistence>/oros-vaults, owned by that user and closed to
//     everyone else, and prints it. It refuses when the data partition's
//     ext4 has no "encrypt" feature: the initramfs script oros-fscrypt
//     turns it on before the partition is mounted (it cannot be turned on
//     while mounted), so a stick has it from its first start with this
//     image on.
//   - "oros-system selftest-vault" (boot tests only, as oros): the same
//     round trip the launcher makes, with a throwaway key: encrypt a
//     folder, write, lock (names and contents unreadable), open, read.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const vaultFolder = "oros-vaults"

// ext4 superblock: at 1024 bytes; s_feature_incompat at 0x60 within it.
const (
	sbOffset      = 1024
	sbIncompat    = 0x60
	sbMagicOffset = 0x38
	ext4Magic     = 0xEF53
	incompatCrypt = 0x10000
)

func vaultBaseMain() int {
	base, err := vaultBase(persistenceTarget(), os.Getenv("SUDO_UID"), os.Getenv("SUDO_GID"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "oros-system vault-base:", err)
		return 1
	}
	fmt.Println(base)
	return 0
}

func vaultBase(target, uid, gid string) (string, error) {
	if target == "" {
		return "", errors.New("no data partition (started without persistence)")
	}
	u, err1 := strconv.Atoi(uid)
	g, err2 := strconv.Atoi(gid)
	if err1 != nil || err2 != nil || u <= 0 || g <= 0 {
		return "", errors.New("run through sudo by the orOS user")
	}
	dev, fstype := vaultMount(target)
	if fstype != "ext4" {
		return "", fmt.Errorf("the data partition is %q, not ext4", fstype)
	}
	if ok, err := ext4Check(dev); err != nil || !ok {
		return "", errors.New("the data partition has no encrypt feature yet (restart the computer once)")
	}
	base := filepath.Join(target, vaultFolder)
	if err := os.Mkdir(base, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(base)
	if err != nil || !fi.IsDir() {
		return "", errors.New(base + " is not a folder")
	}
	if err := os.Lchown(base, u, g); err != nil {
		return "", err
	}
	if err := os.Chmod(base, 0o700); err != nil {
		return "", err
	}
	return base, nil
}

// vaultMount: the device and file system type mounted at target.
var vaultMount = func(target string) (string, string) {
	out, err := run("findmnt", "-n", "-o", "SOURCE,FSTYPE", "--mountpoint", target)
	f := strings.Fields(out)
	if err != nil || len(f) < 2 {
		return "", ""
	}
	return f[len(f)-2], f[len(f)-1]
}

var ext4Check = ext4HasEncrypt

func ext4HasEncrypt(dev string) (bool, error) {
	f, err := os.Open(dev)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sb := make([]byte, 1024)
	if _, err := f.ReadAt(sb, sbOffset); err != nil {
		return false, err
	}
	return superblockHasEncrypt(sb)
}

func superblockHasEncrypt(sb []byte) (bool, error) {
	if len(sb) < 0x64 || binary.LittleEndian.Uint16(sb[sbMagicOffset:]) != ext4Magic {
		return false, errors.New("not an ext file system")
	}
	return binary.LittleEndian.Uint32(sb[sbIncompat:])&incompatCrypt != 0, nil
}

// ---------- boot self-test ----------

// fscrypt ioctls (linux/fscrypt.h; the same numbers on amd64 and arm64).
const (
	fsIocSetPolicy  = 0x800c6613
	fsIocAddKey     = 0xc0506617
	fsIocRemoveKey  = 0xc0406618
	keySpecIdent    = 2
	policyV2        = 2
	modeAES256XTS   = 1
	modeAES256CTS   = 4
	policyPad32     = 3
	removeFilesBusy = 1
)

type keySpec struct {
	Type  uint32
	_     uint32
	Ident [16]byte
	_     [16]byte
}

type addKeyArg struct {
	Spec    keySpec
	RawSize uint32
	KeyID   uint32
	_       [8]uint32
	Raw     [64]byte
}

type removeKeyArg struct {
	Spec  keySpec
	Flags uint32
	_     [5]uint32
}

type policyV2Arg struct {
	Version, Contents, Filenames, Flags, Log2DUS uint8
	_                                            [3]uint8
	Ident                                        [16]byte
}

func ioctlPath(p string, req uintptr, arg unsafe.Pointer) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func selftestVaultMain() int {
	home, _ := os.UserHomeDir()
	var log bytes.Buffer
	err := selftestVault(&log)
	if err != nil {
		fmt.Fprintf(&log, "FAIL: %v\n", err)
	} else {
		fmt.Fprintln(&log, "PASS")
	}
	_ = os.WriteFile(filepath.Join(home, ".oros-selftest-vault"), log.Bytes(), 0o644)
	if err != nil {
		return 1
	}
	return 0
}

func selftestVault(log *bytes.Buffer) error {
	out, err := run("sudo", "-n", "/opt/oros/oros-system", "vault-base")
	if err != nil {
		return fmt.Errorf("vault-base: %v %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	base := strings.TrimSpace(lines[len(lines)-1])
	fmt.Fprintln(log, "base:", base)
	dir, err := os.MkdirTemp(base, "selftest")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var add addKeyArg
	add.Spec.Type = keySpecIdent
	add.RawSize = 64
	if _, err := rand.Read(add.Raw[:]); err != nil {
		return err
	}
	raw := add.Raw
	if err := ioctlPath(dir, fsIocAddKey, unsafe.Pointer(&add)); err != nil {
		return fmt.Errorf("add key: %v", err)
	}
	id := add.Spec.Ident
	p := policyV2Arg{Version: policyV2, Contents: modeAES256XTS, Filenames: modeAES256CTS, Flags: policyPad32, Ident: id}
	if err := ioctlPath(dir, fsIocSetPolicy, unsafe.Pointer(&p)); err != nil {
		return fmt.Errorf("set policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("orOS vault"), 0o600); err != nil {
		return err
	}
	rm := removeKeyArg{Spec: keySpec{Type: keySpecIdent, Ident: id}}
	if err := ioctlPath(base, fsIocRemoveKey, unsafe.Pointer(&rm)); err != nil || rm.Flags&removeFilesBusy != 0 {
		return fmt.Errorf("lock: %v busy=%v", err, rm.Flags&removeFilesBusy != 0)
	}
	names, _ := os.ReadDir(dir)
	if len(names) != 1 || names[0].Name() == "secret.txt" {
		return fmt.Errorf("locked folder shows %v", names)
	}
	if _, err := os.ReadFile(filepath.Join(dir, names[0].Name())); err == nil {
		return errors.New("locked file was readable")
	}
	fmt.Fprintln(log, "locked: ok")
	add = addKeyArg{RawSize: 64, Raw: raw}
	add.Spec.Type = keySpecIdent
	if err := ioctlPath(dir, fsIocAddKey, unsafe.Pointer(&add)); err != nil {
		return fmt.Errorf("add key again: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "secret.txt"))
	if err != nil || string(b) != "orOS vault" {
		return fmt.Errorf("open again: %q %v", b, err)
	}
	fmt.Fprintln(log, "opened: ok")
	rm = removeKeyArg{Spec: keySpec{Type: keySpecIdent, Ident: id}}
	_ = ioctlPath(base, fsIocRemoveKey, unsafe.Pointer(&rm))
	return nil
}
