package main

// NVIDIA's own driver, wave G5 (2026-11-22, Chris: full NVIDIA support,
// older cards too, as automatic as possible).
//
// The image does not carry the driver: scripts/nvidia-pack.sh builds it
// for the image's kernel as a systemd-sysext image (orOS-USB-<version>-
// nvidia.raw, a release file) and pins it in /opt/oros/nvidia.json (size,
// SHA-256, NVIDIA's list of the cards it supports). On a computer with
// such a card:
//
//   - "nvidia --install" (root, through sudo; the System panel, or by
//     itself at the session start when nothing needs the person: see
//     nvidiaAuto) downloads the pack into /home/.oros-gpu (root's folder on
//     the persistence partition) and checks it, writes /oros-system/gpu.cfg
//     next to the system updates' boot.cfg, which makes GRUB start the
//     next boots with nouveau off and NVIDIA's modesetting on, and, with
//     UEFI Secure Boot on, asks the firmware to enrol the orOS key with a
//     one-time code (MokManager, the blue screen at the next start).
//   - "nvidia --boot" (oros-nvidia.service, before the session) merges the
//     pack over /usr and loads the modules. If anything fails it unmerges
//     the pack and loads nouveau again, so the screen always comes up.
//   - "oros-system update" brings the pack of the new system too
//     (update.json "nvidia"), when one is installed.
//
// Older cards (GeForce 600/700 "Kepler", NVIDIA's 470 branch, which Debian
// 13 no longer builds) are not in the list: they stay on Mesa's nouveau.

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	nvidiaPinFile      = "/opt/oros/nvidia.json"
	nvidiaCert         = "/opt/oros/oros-mok.der"
	nvidiaStateFile    = "state.json"
	nvidiaRunFile      = "/run/oros-nvidia.json"
	nvidiaProgressFile = "/run/oros-nvidia-progress.json"
	nvidiaGrubFile     = "gpu.cfg"
	// nvidiaArgs are the kernel options gpu.cfg gives GRUB's ${oros_gpu}.
	nvidiaArgs = "modprobe.blacklist=nouveau nvidia-drm.modeset=1 nvidia-drm.fbdev=1"
)

// nvidiaDir is root's folder for the pack on the persistence partition.
var nvidiaDir = "/home/.oros-gpu"

type nvidiaPin struct {
	File      string   `json:"file"`
	Size      int64    `json:"size"`
	SHA256    string   `json:"sha256"`
	Driver    string   `json:"driver"`
	Kernel    string   `json:"kernel"`
	URL       string   `json:"url"`
	Ephemeral bool     `json:"ephemeral"`
	DevIDs    []string `json:"devids"`
}

func readNvidiaPin() (nvidiaPin, error) {
	var p nvidiaPin
	b, err := os.ReadFile(nvidiaPinFile)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if filepath.Base(p.File) != p.File || !strings.HasSuffix(p.File, "-nvidia.raw") || len(p.SHA256) != 64 || p.Size <= 0 {
		return p, errors.New("bad nvidia.json")
	}
	return p, nil
}

// supports: the driver knows this card (device id "0x1c8d" against
// NVIDIA's list, which writes "0x1C8D").
func (p nvidiaPin) supports(c gpuCard) bool {
	if c.Vendor != "nvidia" {
		return false
	}
	id := strings.ToLower(c.DeviceID)
	for _, d := range p.DevIDs {
		if strings.ToLower(d) == id {
			return true
		}
	}
	return false
}

// nvidiaCardFor is the first NVIDIA card the pack supports, nil if none.
func nvidiaCardFor(p nvidiaPin, cards []gpuCard) *gpuCard {
	for i := range cards {
		if p.supports(cards[i]) {
			return &cards[i]
		}
	}
	return nil
}

type nvidiaState struct {
	Version string `json:"version"` // the image the installed pack is for
	File    string `json:"file"`
	// Mok: "" (Secure Boot off or key already there), "pending" (asked;
	// the person types Code in the blue screen at the next start).
	Mok  string `json:"mok,omitempty"`
	Code string `json:"code,omitempty"`
}

func readNvidiaState() nvidiaState {
	var s nvidiaState
	if b, err := os.ReadFile(filepath.Join(nvidiaDir, nvidiaStateFile)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func writeNvidiaState(s nvidiaState) error {
	b, _ := json.Marshal(s)
	p := filepath.Join(nvidiaDir, nvidiaStateFile)
	if err := os.WriteFile(p+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// ---------- Secure Boot ----------

var secureBootVar = "/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-e98c6c8f2ad8"

// secureBootOn: the firmware enforces Secure Boot (the variable's 5th
// byte; the first 4 are its attributes).
func secureBootOn() bool {
	b, err := os.ReadFile(secureBootVar)
	return err == nil && len(b) >= 5 && b[4] == 1
}

// mokState reads mokutil's answer for the orOS certificate: "enrolled",
// "pending" (asked, waits for the blue screen) or "" (not there).
func mokState() string {
	out, _ := run("mokutil", "--test-key", nvidiaCert)
	return parseMokTest(out)
}

func parseMokTest(out string) string {
	o := strings.ToLower(out)
	switch {
	case strings.Contains(o, "already enrolled"):
		return "enrolled"
	case strings.Contains(o, "enrollment request"):
		return "pending"
	}
	return ""
}

// mokCode is the one-time code for the blue screen: 8 digits, easy to type
// on any keyboard layout (MokManager knows only the US one).
func mokCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%08d", n.Int64()), nil
}

// mokRequest asks the firmware to enrol the orOS key at the next start.
func mokRequest() (string, error) {
	code, err := mokCode()
	if err != nil {
		return "", err
	}
	hash, err := run("mokutil", "--generate-hash="+code)
	if err != nil {
		return "", cmdErr("mokutil --generate-hash", hash, err)
	}
	f, err := os.CreateTemp("", "oros-mok-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, _ = f.WriteString(strings.TrimSpace(hash))
	f.Close()
	if out, err := run("mokutil", "--import", nvidiaCert, "--hash-file", f.Name()); err != nil {
		return "", cmdErr("mokutil --import", out, err)
	}
	return code, nil
}

// ---------- install (root) ----------

type nvidiaOpts struct {
	source, progress string
	force            bool
}

func nvidiaMain(args []string) int {
	if len(args) >= 1 && args[0] == "--boot" {
		nvidiaBoot()
		return 0
	}
	if len(args) == 1 && args[0] == "--remove" {
		if err := nvidiaRemove(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 2 && args[0] == "--selftest" {
		return nvidiaSelftest(args[1])
	}
	if len(args) == 1 && args[0] == "--auto" {
		nvidiaAuto()
		return 0
	}
	if len(args) == 0 {
		b, _ := json.MarshalIndent(nvidiaStatus(), "", "  ")
		fmt.Println(string(b))
		return 0
	}
	fl := flag.NewFlagSet("nvidia", flag.ContinueOnError)
	var o nvidiaOpts
	install := fl.Bool("install", false, "download the driver for this computer's NVIDIA card")
	fl.StringVar(&o.source, "source", "", "the pack file instead of the release (tests)")
	fl.StringVar(&o.progress, "progress", nvidiaProgressFile, "progress file (JSON)")
	fl.BoolVar(&o.force, "force", false, "install without a supported card (tests)")
	if err := fl.Parse(args); err != nil || !*install {
		fmt.Fprintln(os.Stderr, "usage: oros-system nvidia [--install | --remove | --boot | --auto]")
		return 2
	}
	// Through sudo only the official release, the fixed progress file and
	// a real card.
	if os.Getenv("SUDO_USER") != "" {
		o = nvidiaOpts{progress: nvidiaProgressFile}
	}
	write := progressWriter(o.progress)
	err := nvidiaInstall(o, func(s string, p int) { write(progress{Step: s, Percent: p}) })
	if err != nil {
		write(progress{Step: "error", Done: true, Error: err.Error()})
		logf("nvidia: %v", err)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	write(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

func nvidiaInstall(o nvidiaOpts, step func(string, int)) error {
	if os.Geteuid() != 0 {
		return errors.New("needs root")
	}
	pin, err := readNvidiaPin()
	if err != nil {
		return fmt.Errorf("this system has no NVIDIA driver: %v", err)
	}
	if !o.force && nvidiaCardFor(pin, readGPUs()) == nil {
		return errors.New("no NVIDIA card that this driver supports")
	}
	target := persistenceTarget()
	if target == "" {
		return errors.New("no persistence partition: the driver needs one to stay")
	}
	if err := os.MkdirAll(nvidiaDir, 0o755); err != nil {
		return err
	}
	_ = os.Chown(nvidiaDir, 0, 0)
	_ = os.Chmod(nvidiaDir, 0o755)
	var st syscall.Statfs_t
	if err := syscall.Statfs(nvidiaDir, &st); err == nil {
		if free := int64(st.Bavail) * int64(st.Bsize); free < pin.Size+64<<20 {
			return fmt.Errorf("not enough space: %d MB free, %d MB needed", free>>20, (pin.Size+64<<20)>>20)
		}
	}
	dst := filepath.Join(nvidiaDir, pin.File)
	if !packOK(dst, pin.Size) {
		step("download", 0)
		if err := fetchPack(o.source, pin, dst, func(n int64) { step("download", int(n*90/pin.Size)) }); err != nil {
			return err
		}
	}
	// Packs of other systems: only the ones the boot menu can still start
	// stay (prunePacks).
	prunePacks(pin.File)

	step("boot", 92)
	if err := writeGrubGPU(target, true); err != nil {
		return err
	}
	s := nvidiaState{Version: currentVersion(), File: pin.File}
	if secureBootOn() {
		step("secureboot", 96)
		switch mokState() {
		case "enrolled":
		case "pending":
			old := readNvidiaState()
			s.Mok, s.Code = "pending", old.Code
			if s.Code == "" {
				// a request made elsewhere: ask again so the code is known
				if code, err := mokRequest(); err == nil {
					s.Code = code
				}
			}
		default:
			code, err := mokRequest()
			if err != nil {
				return err
			}
			s.Mok, s.Code = "pending", code
		}
	}
	syscall.Sync()
	return writeNvidiaState(s)
}

// packOK: the pack is there with the pinned size (it was checked against
// the SHA-256 when it was downloaded, into root's own folder).
func packOK(p string, size int64) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() == size
}

// fetchPack downloads the pack (or copies it from src) and keeps it only
// when size and SHA-256 match the pin.
func fetchPack(src string, pin nvidiaPin, dst string, progress func(int64)) error {
	tmp := dst + ".part"
	defer os.Remove(tmp)
	var in io.ReadCloser
	if src != "" {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		in = f
	} else {
		if !strings.HasPrefix(pin.URL, "https://github.com/koulaxizis/oros-usb/releases/download/") {
			return fmt.Errorf("unexpected address %q", pin.URL)
		}
		c := &http.Client{Timeout: 2 * time.Hour}
		res, err := c.Get(pin.URL)
		if err != nil {
			return err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return fmt.Errorf("%s: %s", pin.File, res.Status)
		}
		in = res.Body
	}
	defer in.Close()
	dir := filepath.Dir(dst)
	if err := copyChecked(in, updateFile{Name: filepath.Base(tmp), Size: pin.Size, SHA256: pin.SHA256}, dir, progress); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// prunePacks removes packs other than keep and the ones the boot menu's
// systems (the slots' versions) still use.
func prunePacks(keep ...string) {
	want := map[string]bool{}
	for _, k := range keep {
		want[k] = true
	}
	if t := persistenceTarget(); t != "" {
		if dir, release, err := slotsDir(t); err == nil {
			for _, s := range []string{"A", "B"} {
				if v, err := os.ReadFile(filepath.Join(dir, updateDir, s, "version")); err == nil {
					want["orOS-USB-"+strings.TrimSpace(string(v))+"-nvidia.raw"] = true
				}
			}
			release()
		}
	}
	l, _ := filepath.Glob(filepath.Join(nvidiaDir, "*-nvidia.raw"))
	sort.Strings(l)
	for _, p := range l {
		if !want[filepath.Base(p)] {
			_ = os.Remove(p)
		}
	}
}

// writeGrubGPU writes or removes /oros-system/gpu.cfg on the partition
// GRUB reads system updates from.
func writeGrubGPU(target string, on bool) error {
	dir, release, err := slotsDir(target)
	if err != nil {
		return err
	}
	defer release()
	base := filepath.Join(dir, updateDir)
	p := filepath.Join(base, nvidiaGrubFile)
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	cfg := "# Written by oros-system nvidia: NVIDIA's own driver (G5). Delete to go back to nouveau.\nset oros_gpu=\"" + nvidiaArgs + "\"\n"
	if err := os.WriteFile(p+".tmp", []byte(cfg), 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func nvidiaRemove() error {
	if os.Geteuid() != 0 {
		return errors.New("needs root")
	}
	if t := persistenceTarget(); t != "" {
		if err := writeGrubGPU(t, false); err != nil {
			return err
		}
	}
	l, _ := filepath.Glob(filepath.Join(nvidiaDir, "*-nvidia.raw"))
	for _, p := range l {
		_ = os.Remove(p)
	}
	_ = os.Remove(filepath.Join(nvidiaDir, nvidiaStateFile))
	syscall.Sync()
	return nil
}

// ---------- boot (oros-nvidia.service, root) ----------

type nvidiaRun struct {
	Loaded bool   `json:"loaded"`
	Driver string `json:"driver,omitempty"`
	Why    string `json:"why,omitempty"` // why it did not load
}

func writeNvidiaRun(r nvidiaRun) {
	b, _ := json.Marshal(r)
	_ = os.WriteFile(nvidiaRunFile, b, 0o644)
}

var procCmdline = "/proc/cmdline"

// nvidiaBoot merges the pack and loads the driver, or brings nouveau back.
func nvidiaBoot() {
	cmd, _ := os.ReadFile(procCmdline)
	if !strings.Contains(" "+string(cmd)+" ", " modprobe.blacklist=nouveau ") {
		return // GRUB did not start with NVIDIA's options (BIOS, or not installed)
	}
	fallback := func(why string) {
		logf("nvidia: %s; nouveau instead", why)
		_, _ = run("systemd-sysext", "unmerge")
		_ = os.Remove("/run/extensions/oros-nvidia.raw")
		_, _ = run("modprobe", "nouveau")
		writeNvidiaRun(nvidiaRun{Why: why})
	}
	pin, err := readNvidiaPin()
	if err != nil {
		fallback("no pin")
		return
	}
	if nvidiaCardFor(pin, readGPUs()) == nil {
		fallback("no supported NVIDIA card in this computer")
		return
	}
	pack := filepath.Join(nvidiaDir, pin.File)
	if !packOK(pack, pin.Size) {
		// a new system whose pack is not here yet (System update brings it)
		fallback("no driver for this system yet")
		return
	}
	if secureBootOn() && mokState() != "enrolled" {
		fallback("Secure Boot: the orOS key is not enrolled yet")
		return
	}
	if err := os.MkdirAll("/run/extensions", 0o755); err != nil {
		fallback(err.Error())
		return
	}
	_ = os.Remove("/run/extensions/oros-nvidia.raw")
	if err := os.Symlink(pack, "/run/extensions/oros-nvidia.raw"); err != nil {
		fallback(err.Error())
		return
	}
	if out, err := run("systemd-sysext", "refresh"); err != nil {
		fallback("systemd-sysext: " + strings.TrimSpace(out))
		return
	}
	_ = os.MkdirAll("/run/modprobe.d", 0o755)
	_ = os.WriteFile("/run/modprobe.d/oros-nvidia.conf", []byte(
		"# oros-system nvidia (G5)\noptions nvidia-drm modeset=1 fbdev=1\noptions nvidia NVreg_PreserveVideoMemoryAllocations=1\n"), 0o644)
	_, _ = run("ldconfig")
	if out, err := run("modprobe", "nvidia-drm"); err != nil {
		fallback("modprobe: " + strings.TrimSpace(out))
		return
	}
	_, _ = run("modprobe", "nvidia-uvm")
	v := readTrim("/sys/module/nvidia/version")
	if v == "" {
		v = readTrim("/sys/module/nvidia_current/version") // Debian's name
	}
	logf("nvidia: driver %s loaded", v)
	writeNvidiaRun(nvidiaRun{Loaded: true, Driver: v})
	// the enrolment is done: the code is no longer needed
	if s := readNvidiaState(); s.Mok != "" {
		s.Mok, s.Code = "", ""
		_ = writeNvidiaState(s)
	}
}

// ---------- status and the automatic install (the person's side) ----------

// nvidiaStatus is "nvidia.status" (and "oros-system nvidia").
func nvidiaStatus() reply {
	r := reply{"ok": true}
	pin, err := readNvidiaPin()
	if err != nil {
		r["available"] = false
		return r
	}
	r["available"] = true
	r["driver"] = pin.Driver
	cards := readGPUs()
	if c := nvidiaCardFor(pin, cards); c != nil {
		r["card"] = c.Slot
	} else {
		for _, c := range cards {
			if c.Vendor == "nvidia" {
				r["unsupported"] = c.DeviceID // e.g. a Kepler card: nouveau
			}
		}
	}
	s := readNvidiaState()
	r["installed"] = s.File != "" && packOK(filepath.Join(nvidiaDir, pin.File), pin.Size)
	if s.File != "" && !r["installed"].(bool) {
		r["needsUpdate"] = true // a new system: its driver comes with System update or Install again
	}
	if s.Mok == "pending" {
		r["mok"] = "pending"
		r["code"] = s.Code
	}
	r["secureBoot"] = secureBootOn()
	r["canInstall"] = persistenceTarget() != ""
	var nr nvidiaRun
	if b, err := os.ReadFile(nvidiaRunFile); err == nil {
		_ = json.Unmarshal(b, &nr)
	}
	r["loaded"] = nr.Loaded
	if nr.Why != "" {
		r["why"] = nr.Why
	}
	return r
}

// nvidiaAuto runs at the session start (oros-session): on a computer with
// a supported NVIDIA card, a persistence partition and nothing for the
// person to do (Secure Boot off, or the orOS key already there), the driver
// is installed by itself and used from the next start. With Secure Boot on
// and no key yet, the blue screen needs the person: the System panel asks
// first. Waits for the network for up to 10 minutes.
func nvidiaAuto() {
	st := nvidiaStatus()
	if st["available"] != true || st["card"] == nil || st["canInstall"] != true || st["installed"] == true {
		return
	}
	if secureBootOn() && mokState() != "enrolled" {
		return
	}
	for i := 0; i < 60; i++ {
		if out, _ := run("nmcli", "-t", "-f", "CONNECTIVITY", "general"); strings.TrimSpace(out) == "full" {
			break
		}
		time.Sleep(10 * time.Second)
	}
	logf("nvidia: installing NVIDIA's driver by itself")
	if out, err := run("sudo", "-n", "/opt/oros/oros-system", "nvidia", "--install"); err != nil {
		logf("nvidia: %v %s", err, strings.TrimSpace(out))
	}
}

// nvidiaSelftest (boot test, root, oros-selftest-nvidia.service): the pack
// this build made merges into this system, carries NVIDIA's libraries,
// Vulkan file and firmware, and its modules are signed and built for the
// running kernel. Nothing is loaded (QEMU has no NVIDIA card). The result
// goes to /home/.oros-selftest-nvidia.txt for test-boot.sh.
func nvidiaSelftest(pack string) int {
	var b strings.Builder
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(&b, "FAIL %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(&b, "ok %s\n", name)
	}
	pin, err := readNvidiaPin()
	check("nvidia.json", err)
	if err == nil {
		st, serr := os.Stat(pack)
		if serr == nil && st.Size() != pin.Size {
			serr = fmt.Errorf("%d bytes, pinned %d", st.Size(), pin.Size)
		}
		check("pack size "+pin.File, serr)
		fmt.Fprintf(&b, "info driver %s, %d cards, ephemeral key %v\n", pin.Driver, len(pin.DevIDs), pin.Ephemeral)
	}
	_ = os.MkdirAll("/run/extensions", 0o755)
	link := "/run/extensions/oros-nvidia.raw"
	_ = os.Remove(link)
	check("link", os.Symlink(pack, link))
	out, err := run("systemd-sysext", "refresh")
	check("systemd-sysext merge", cmdErrIf("systemd-sysext", out, err))
	for _, f := range []string{
		"/usr/lib/x86_64-linux-gnu/libGLX_nvidia.so.0",
		"/usr/lib/x86_64-linux-gnu/libEGL_nvidia.so.0",
		"/usr/share/vulkan/icd.d/nvidia_icd.json",
		"/usr/share/glvnd/egl_vendor.d/10_nvidia.json",
	} {
		_, err := os.Stat(f)
		check(f, err)
	}
	fw, _ := filepath.Glob("/usr/lib/firmware/nvidia/*/gsp*.bin")
	if len(fw) == 0 {
		check("GSP firmware", errors.New("none"))
	} else {
		check("GSP firmware", nil)
	}
	// Debian's names (nvidia-current*) behind the usual ones, with the
	// pack's options: what nvidiaBoot's modprobe will load.
	dry, err := run("modprobe", "-n", "-v", "nvidia-drm")
	if err == nil && !strings.Contains(dry, "modeset=1") {
		err = fmt.Errorf("%q", strings.TrimSpace(dry))
	}
	check("modprobe nvidia-drm (dry run, modeset=1)", err)
	signer, err := run("modinfo", "-F", "signer", "nvidia")
	if err == nil && strings.TrimSpace(signer) == "" {
		err = errors.New("not signed")
	}
	check("nvidia.ko signed by "+strings.TrimSpace(signer), err)
	vm, err := run("modinfo", "-F", "vermagic", "nvidia")
	uname, _ := run("uname", "-r")
	if err == nil && !strings.HasPrefix(strings.TrimSpace(vm), strings.TrimSpace(uname)+" ") {
		err = fmt.Errorf("built for %q, running %q", strings.TrimSpace(vm), strings.TrimSpace(uname))
	}
	check("nvidia.ko for this kernel", err)
	mok, _ := run("mokutil", "--test-key", nvidiaCert)
	fmt.Fprintf(&b, "info secure boot %v, orOS key: %q\n", secureBootOn(), parseMokTest(mok))
	_, _ = run("systemd-sysext", "unmerge")
	_ = os.Remove(link)
	if failed {
		b.WriteString("FAIL\n")
	} else {
		b.WriteString("PASS\n")
	}
	_ = os.WriteFile("/home/.oros-selftest-nvidia.txt", []byte(b.String()), 0o644)
	if failed {
		return 1
	}
	return 0
}

func cmdErrIf(name, out string, err error) error {
	if err == nil {
		return nil
	}
	return cmdErr(name, out, err)
}
