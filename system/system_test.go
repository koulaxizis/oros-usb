package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitTerse(t *testing.T) {
	got := splitTerse(`*:Café\:Guest:72:WPA2`)
	want := []string{"*", "Café:Guest", "72", "WPA2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestParseWifiList(t *testing.T) {
	out := " :Home:40:WPA2\n*:Home:70:WPA2\n :Open Net:55:\n ::80:WPA2\n :Home:90:WPA2\n"
	l := parseWifiList(out)
	if len(l) != 2 {
		t.Fatalf("got %+v", l)
	}
	if !l[0].InUse || l[0].Signal != 70 || !l[0].Secure {
		t.Fatalf("connected network lost: %+v", l[0])
	}
	if l[1].SSID != "Open Net" || l[1].Secure {
		t.Fatalf("open network: %+v", l[1])
	}
}

func TestParseVolume(t *testing.T) {
	if v, m, ok := parseVolume("Volume: 0.40"); !ok || v != 40 || m {
		t.Fatal(v, m, ok)
	}
	if v, m, ok := parseVolume("Volume: 1.00 [MUTED]\n"); !ok || v != 100 || !m {
		t.Fatal(v, m, ok)
	}
	if _, _, ok := parseVolume("no sink"); ok {
		t.Fatal("parsed nonsense")
	}
}

func TestPickTargets(t *testing.T) {
	j := `{"blockdevices":[
	 {"name":"sda","size":512110190592,"model":"Samsung SSD ","tran":"sata","type":"disk","ro":false},
	 {"name":"sdb","size":31914983424,"model":"USB Stick","tran":"usb","type":"disk","ro":false},
	 {"name":"sr0","size":1073741312,"model":"DVD","tran":"sata","type":"rom","ro":false},
	 {"name":"zram0","size":8000000000,"model":null,"tran":null,"type":"disk","ro":false},
	 {"name":"mmcblk0","size":4000000000,"model":null,"tran":null,"type":"disk","ro":false}]}`
	d, err := pickTargets(j, "sdb")
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0].Name != "sda" || d[0].Model != "Samsung SSD" {
		t.Fatalf("got %+v", d)
	}
}

func TestPatchBootConfigs(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "boot", "grub"), 0o755))
	cfg := filepath.Join(root, "boot", "grub", "grub.cfg")
	must(t, os.WriteFile(cfg, []byte("linux /live/vmlinuz boot=live components quiet persistence\n"), 0o644))
	must(t, patchBootConfigs(root))
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "boot=live persistence-label=orosdisk components") {
		t.Fatalf("got %s", b)
	}
	if err := patchBootConfigs(root); err == nil {
		t.Fatal("second patch should find nothing to change")
	}
}

func TestCopyTreeSkipsLinks(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	must(t, os.MkdirAll(filepath.Join(src, "EFI", "boot"), 0o755))
	must(t, os.WriteFile(filepath.Join(src, "EFI", "boot", "bootx64.efi"), []byte("efi"), 0o644))
	must(t, os.Symlink(".", filepath.Join(src, "debian")))
	var n int64
	must(t, copyTree(src, dst, func(k int64) { n += k }))
	if n != 3 {
		t.Fatalf("copied %d bytes", n)
	}
	if _, err := os.Lstat(filepath.Join(dst, "debian")); err == nil {
		t.Fatal("link copied")
	}
	if findFold(dst, "efi/BOOT/BootX64.EFI") == "" {
		t.Fatal("case-insensitive lookup failed")
	}
}

func TestHostProtocol(t *testing.T) {
	var in bytes.Buffer
	msg := []byte(`{"cmd":"nope"}`)
	must(t, binary.Write(&in, binary.LittleEndian, uint32(len(msg))))
	in.Write(msg)
	var out bytes.Buffer
	must(t, serveHost(&in, &out))
	var n uint32
	must(t, binary.Read(&out, binary.LittleEndian, &n))
	var r map[string]any
	must(t, json.Unmarshal(out.Bytes()[:n], &r))
	if r["ok"] != false || r["error"] != "unknown command" {
		t.Fatalf("got %v", r)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenWeb(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	out := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "chromium")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldA := webBrowser, webAdblock
	webBrowser = stub
	webAdblock = filepath.Join(dir, "ubol")
	defer func() { webBrowser, webAdblock = old, oldA }()
	must(t, os.MkdirAll(webAdblock, 0o755))
	must(t, os.WriteFile(filepath.Join(webAdblock, "manifest.json"), []byte("{}"), 0o644))
	if r := handle(request{Cmd: "web.open"}); r["ok"] != true {
		t.Fatalf("web.open: %v", r)
	}
	var b []byte
	for i := 0; i < 50 && len(b) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ = os.ReadFile(out)
	}
	if !strings.Contains(string(b), "--user-data-dir="+filepath.Join(dir, "oros", "data", "web")) {
		t.Fatalf("args: %q", b)
	}
	if !strings.Contains(string(b), "--load-extension="+webAdblock) {
		t.Fatalf("no adblocker in args: %q", b)
	}
}

func TestUpdateHelpers(t *testing.T) {
	if s := runningSlot("BOOT_IMAGE=/x boot=live live-media-path=/oros-system/B quiet"); s != "B" {
		t.Fatalf("slot %q", s)
	}
	if s := runningSlot("boot=live live-media-path=/live quiet"); s != "" {
		t.Fatalf("slot %q", s)
	}
	got := bootArgs("BOOT_IMAGE=/live/vmlinuz boot=live components quiet splash persistence live-media-path=/oros-system/A live-media=/dev/sdb4", "abcd-1234", "B")
	want := "boot=live components quiet splash persistence live-media=/dev/disk/by-uuid/abcd-1234 live-media-path=/oros-system/B toram=filesystem.squashfs"
	if got != want {
		t.Fatalf("args:\n%s\n%s", got, want)
	}
	cfg := bootConfig([]bootEntry{{"orOS 2", "B", "boot=live"}, {"orOS 1 (previous)", "A", "boot=live"}})
	for _, s := range []string{`menuentry "orOS 2" {`, "linux ($orosupd)/oros-system/B/vmlinuz boot=live", "initrd ($orosupd)/oros-system/A/initrd.img"} {
		if !strings.Contains(cfg, s) {
			t.Fatalf("boot.cfg lacks %q:\n%s", s, cfg)
		}
	}
}

func writeRelease(t *testing.T, dir, version string, damage bool) {
	t.Helper()
	m := updateManifest{Version: version}
	for i, n := range updateFiles {
		data := []byte(strings.Repeat(n, 1000+i))
		sum := sha256.Sum256(data)
		if damage && i == 0 {
			data[0] ^= 1
		}
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
		m.Files = append(m.Files, updateFile{Name: n, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "update.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdate(t *testing.T) {
	old := partitionUUID
	partitionUUID = func(string) (string, error) { return "u-1", nil }
	defer func() { partitionUUID = old }()
	src, target := t.TempDir(), t.TempDir()
	step := func(string, int) {}
	cmd := "BOOT_IMAGE=/live/vmlinuz boot=live components quiet persistence"

	writeRelease(t, src, "2026.11.01", true)
	if err := update(updateOpts{source: src, target: target, cmdline: cmd}, step); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("damaged download: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "oros-system", "boot.cfg")); err == nil {
		t.Fatal("boot.cfg written for a damaged download")
	}

	writeRelease(t, src, "2026.11.01", false)
	if err := update(updateOpts{source: src, target: target, cmdline: cmd}, step); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(target, "oros-system", "boot.cfg"))
	if !strings.Contains(string(cfg), "/oros-system/A/vmlinuz boot=live components quiet persistence live-media=/dev/disk/by-uuid/u-1 live-media-path=/oros-system/A toram=filesystem.squashfs") {
		t.Fatalf("first update:\n%s", cfg)
	}

	// Running from slot A, the next update goes to B and keeps A as previous.
	writeRelease(t, src, "2026.12.01", false)
	if err := update(updateOpts{source: src, target: target, cmdline: cmd + " live-media-path=/oros-system/A"}, step); err != nil {
		t.Fatal(err)
	}
	cfg, _ = os.ReadFile(filepath.Join(target, "oros-system", "boot.cfg"))
	if !strings.Contains(string(cfg), `menuentry "orOS 2026.12.01"`) || !strings.Contains(string(cfg), `menuentry "orOS 2026.11.01 (previous)"`) ||
		strings.Index(string(cfg), "/B/vmlinuz") > strings.Index(string(cfg), "/A/vmlinuz") {
		t.Fatalf("second update:\n%s", cfg)
	}
	if v, _ := os.ReadFile(filepath.Join(target, "oros-system", "B", "version")); strings.TrimSpace(string(v)) != "2026.12.01" {
		t.Fatalf("B version %q", v)
	}
}

func TestInstalledSystem(t *testing.T) {
	dir := t.TempDir()
	old := cmdlineFile
	defer func() { cmdlineFile = old }()
	cmdlineFile = filepath.Join(dir, "cmdline")
	for cl, want := range map[string]bool{
		"boot=live components persistence persistence-label=orosdisk quiet": true,
		"boot=live components persistence quiet":                            false,
		"boot=live persistence-label=orosdisk2":                             false,
	} {
		if err := os.WriteFile(cmdlineFile, []byte(cl+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := installedSystem(); got != want {
			t.Fatalf("%q: %v", cl, got)
		}
	}
}

func TestRemoveBrowserLocks(t *testing.T) {
	root := t.TempDir()
	prof := filepath.Join(root, "oros", "data", "profile")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink("debian-1234", filepath.Join(prof, "SingletonLock"))
	_ = os.WriteFile(filepath.Join(prof, "SingletonCookie"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(prof, "Preferences"), []byte("{}"), 0o644)
	removeBrowserLocks(root)
	if _, err := os.Lstat(filepath.Join(prof, "SingletonLock")); err == nil {
		t.Fatal("lock kept")
	}
	if _, err := os.Lstat(filepath.Join(prof, "SingletonCookie")); err == nil {
		t.Fatal("cookie kept")
	}
	if _, err := os.Stat(filepath.Join(prof, "Preferences")); err != nil {
		t.Fatal("preferences removed")
	}
}

func TestFetchManifestNoRelease(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	old := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = old }()
	if _, err := fetchManifest(srv.URL + "/update.json"); !errors.Is(err, errNoRelease) {
		t.Fatalf("err %v", err)
	}
}

func TestPickVolumes(t *testing.T) {
	j := `{"blockdevices":[
	{"name":"nvme0n1","path":"/dev/nvme0n1","pkname":null,"type":"disk","size":512110190592,"fstype":null,"label":null,"mountpoint":null,"rm":false,"hotplug":false,"tran":"nvme","model":"Samsung SSD 980"},
	{"name":"nvme0n1p1","path":"/dev/nvme0n1p1","pkname":"nvme0n1","type":"part","size":272629760,"fstype":"vfat","label":"SYSTEM","mountpoint":null,"rm":false,"hotplug":false,"tran":null,"model":null},
	{"name":"nvme0n1p3","path":"/dev/nvme0n1p3","pkname":"nvme0n1","type":"part","size":510000000000,"fstype":"ntfs","label":"Windows","mountpoint":null,"rm":false,"hotplug":false,"tran":null,"model":null},
	{"name":"nvme0n1p4","path":"/dev/nvme0n1p4","pkname":"nvme0n1","type":"part","size":700000000,"fstype":"ntfs","label":"Recovery","mountpoint":null,"rm":false,"hotplug":false,"tran":null,"model":null},
	{"name":"sda","path":"/dev/sda","pkname":null,"type":"disk","size":32010928128,"fstype":null,"label":null,"mountpoint":null,"rm":true,"hotplug":true,"tran":"usb","model":"Flash Drive"},
	{"name":"sda1","path":"/dev/sda1","pkname":"sda","type":"part","size":1500000000,"fstype":"iso9660","label":"orOS","mountpoint":"/run/live/medium","rm":true,"hotplug":true,"tran":null,"model":null},
	{"name":"sda3","path":"/dev/sda3","pkname":"sda","type":"part","size":15000000000,"fstype":"ext4","label":"persistence","mountpoint":null,"rm":true,"hotplug":true,"tran":null,"model":null},
	{"name":"sdb","path":"/dev/sdb","pkname":null,"type":"disk","size":64000000000,"fstype":"exfat","label":"PHOTOS","mountpoint":"/media/oros/PHOTOS","rm":true,"hotplug":true,"tran":"usb","model":"Ultra"},
	{"name":"sr0","path":"/dev/sr0","pkname":null,"type":"rom","size":1073741312,"fstype":"iso9660","label":"CD","mountpoint":null,"rm":true,"hotplug":true,"tran":"sata","model":"DVD"},
	{"name":"loop0","path":"/dev/loop0","pkname":null,"type":"loop","size":1300000000,"fstype":"squashfs","label":null,"mountpoint":"/run/live/rootfs","rm":false,"hotplug":false,"tran":null,"model":null}
	]}`
	vs, err := pickVolumes(j, map[string]bool{"sda": true, "sda1": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %+v", vs)
	}
	w, p := vs[0], vs[1]
	if w.Path != "/dev/nvme0n1p3" || w.Label != "Windows" || !w.Internal || w.USB || w.Model != "Samsung SSD 980" || w.Mount != "" {
		t.Fatalf("windows %+v", w)
	}
	if p.Path != "/dev/sdb" || !p.USB || p.Internal || p.Mount != "/media/oros/PHOTOS" {
		t.Fatalf("stick %+v", p)
	}
	// A stick without partitions has no parent; an empty name in the
	// system set (lsblk PKNAME of a whole disk) must not hide it.
	vs, err = pickVolumes(j, map[string]bool{"sda": true, "sda1": true, "": true})
	if err != nil || len(vs) != 2 || vs[1].Path != "/dev/sdb" {
		t.Fatalf("whole-disk stick hidden: %+v %v", vs, err)
	}
	if d := mountDir(volume{Path: "/dev/sdc1", Label: "My Disk/../x"}); d != filepath.Join(mediaRoot, "My_Disk_.._x") {
		t.Fatalf("dir %q", d)
	}
	if d := mountDir(volume{Path: "/dev/sdc1", Label: ".."}); d != filepath.Join(mediaRoot, "sdc1") {
		t.Fatalf("dir %q", d)
	}
}

// TestMountVolume mounts real filesystems (root and loop devices only, as
// in CI): writable, except NTFS inside the computer or holding Windows.
func TestMountVolume(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("losetup"); err != nil {
		t.Skip("no losetup")
	}
	old := mediaRoot
	mediaRoot = t.TempDir()
	defer func() { mediaRoot = old }()
	mkfsFor := map[string][]string{
		"vfat": {"mkfs.vfat", "-n", "STICK"},
		"ext4": {"mkfs.ext4", "-q", "-L", "LINUX"},
		"ntfs": {"mkntfs", "-Q", "-q", "-F", "-L", "Windows"},
	}
	for _, c := range []struct {
		name, fs     string
		usb, windows bool
		wantWritable bool
	}{
		{"vfat stick", "vfat", true, false, true},
		{"ext4 disk", "ext4", false, false, true},
		{"ntfs internal", "ntfs", false, false, false},
		{"ntfs stick", "ntfs", true, false, true},
		{"ntfs stick with Windows", "ntfs", true, true, false},
	} {
		mkfs := mkfsFor[c.fs]
		if _, err := exec.LookPath(mkfs[0]); err != nil {
			t.Logf("%s: no %s", c.name, mkfs[0])
			continue
		}
		if b, _ := os.ReadFile("/proc/filesystems"); c.fs != "ntfs" && !strings.Contains(string(b), "\t"+c.fs+"\n") {
			if exec.Command("modprobe", c.fs).Run() != nil {
				t.Logf("%s: not in this kernel", c.name)
				continue
			}
		}
		img := filepath.Join(t.TempDir(), c.fs+".img")
		if err := os.WriteFile(img, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(img, 64<<20); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(mkfs[0], append(mkfs[1:], img)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", c.name, err, out)
		}
		out, err := exec.Command("losetup", "-f", "--show", img).Output()
		if err != nil {
			t.Skipf("losetup: %v", err)
		}
		dev := strings.TrimSpace(string(out))
		if c.windows {
			// Put Windows' folder on it first.
			tmp := t.TempDir()
			if out, err := exec.Command("mount", "-t", "ntfs-3g", dev, tmp).CombinedOutput(); err != nil {
				t.Logf("%s: cannot prepare: %v %s", c.name, err, out)
				_ = exec.Command("losetup", "-d", dev).Run()
				continue
			}
			_ = os.MkdirAll(filepath.Join(tmp, "Windows", "System32"), 0o755)
			_ = exec.Command("umount", tmp).Run()
		}
		v := volume{Path: dev, Label: "Disk-" + c.fs, FSType: c.fs, USB: c.usb, Internal: !c.usb}
		dir, err := mountVolume(v, "1000", "1000")
		if err != nil {
			_ = exec.Command("losetup", "-d", dev).Run()
			t.Fatalf("%s: %v", c.name, err)
		}
		if dir != filepath.Join(mediaRoot, "Disk-"+c.fs) {
			t.Errorf("%s: dir %s", c.name, dir)
		}
		err = os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644)
		if c.wantWritable && err != nil {
			t.Errorf("%s: not writable: %v", c.name, err)
		}
		if !c.wantWritable && err == nil {
			t.Errorf("%s: mounted writable", c.name)
		}
		if readOnlyMounts()[dir] == c.wantWritable {
			t.Errorf("%s: /proc/mounts says read-only=%v", c.name, !c.wantWritable)
		}
		v.Mount = dir
		if err := unmountVolume(v); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s: mount folder left", c.name)
		}
		_ = exec.Command("losetup", "-d", dev).Run()
	}
}

func TestDiskOpen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	out := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "chromium")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$@\" > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldB, oldR := webBrowser, rootRun
	webBrowser = stub
	var got []string
	rootRun = func(args ...string) (string, error) { got = args; return "/media/oros/Windows\n", nil }
	defer func() { webBrowser, rootRun = oldB, oldR }()
	r := handle(request{Cmd: "disks.open", Disk: "/dev/nvme0n1p3"})
	if r["ok"] != true || r["mount"] != "/media/oros/Windows" {
		t.Fatalf("open: %v", r)
	}
	if strings.Join(got, " ") != "mount --dev /dev/nvme0n1p3" {
		t.Fatalf("root args %q", got)
	}
	var b []byte
	for i := 0; i < 50 && len(b) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ = os.ReadFile(out)
	}
	if !strings.Contains(string(b), "file:///media/oros/Windows/") {
		t.Fatalf("args %q", b)
	}
	if r := handle(request{Cmd: "disks.open", Disk: "downloads"}); r["ok"] != true || r["mount"] != filepath.Join(dir, "Downloads") {
		t.Fatalf("downloads: %v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "Downloads")); err != nil {
		t.Fatal(err)
	}
}

func TestReport(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "oros", "data", "profile")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&log, "line %d\n", i)
	}
	_ = os.WriteFile(filepath.Join(data, "chrome_debug.log"), []byte(log.String()), 0o644)
	_ = os.WriteFile(filepath.Join(home, "oros", "data", "session.log"), []byte("== start\n"), 0o644)
	p, err := writeReport(home, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"===== uname -a", "===== session.log\n== start\n", "line 999\n", "===== install.log\n(none)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "line 599\n") {
		t.Fatal("chrome_debug.log not cut to its last lines")
	}
	if filepath.Dir(p) != filepath.Join(home, "Downloads") {
		t.Fatalf("path %s", p)
	}
}

func TestPingWritesAliveQuietly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "oros", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	alive := filepath.Join(t.TempDir(), "oros-alive")
	t.Setenv("OROS_ALIVE", alive)
	if r := handle(request{Cmd: "ping"}); r["ok"] != true {
		t.Fatalf("ping: %v", r)
	}
	first, err := os.ReadFile(alive)
	if err != nil || len(first) < 10 {
		t.Fatalf("no alive file: %q %v", first, err)
	}
	time.Sleep(time.Millisecond)
	handle(request{Cmd: "ping"})
	if second, _ := os.ReadFile(alive); bytes.Equal(first, second) {
		t.Fatal("a new ping must change the file, the watchdog compares contents")
	}
	if _, err := os.Stat(filepath.Join(home, "oros", "data", "oros-system.log")); err == nil {
		t.Fatal("pings must not fill oros-system.log")
	}
	t.Setenv("OROS_ALIVE", "")
	if r := handle(request{Cmd: "ping"}); r["ok"] != true {
		t.Fatalf("ping without a watchdog: %v", r)
	}
}

func TestBridgeCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code := filepath.Join(t.TempDir(), "oros-bridge-code")
	t.Setenv("OROS_BRIDGE_CODE", code)
	t.Setenv("OROS_ALIVE", "")
	if r := handle(request{Cmd: "ping"}); r["bridgeCode"] != nil {
		t.Fatalf("no file, no code: %v", r)
	}
	if err := os.WriteFile(code, []byte("042917\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := handle(request{Cmd: "ping"}); r["bridgeCode"] != "042917" {
		t.Fatalf("ping carries the code: %v", r)
	}
	if r := handle(request{Cmd: "bridge.code"}); r["ok"] != true || r["code"] != "042917" {
		t.Fatalf("bridge.code: %v", r)
	}
	for _, bad := range []string{"12345\n", "1234567\n", "12a456\n", ""} {
		os.WriteFile(code, []byte(bad), 0o600)
		if r := handle(request{Cmd: "bridge.code"}); r["code"] != "" {
			t.Fatalf("%q is not a code: %v", bad, r)
		}
	}
	os.Remove(code)
	if r := handle(request{Cmd: "bridge.code"}); r["ok"] != true || r["code"] != "" {
		t.Fatalf("gone: %v", r)
	}
}

func TestWatchdogLinesInReport(t *testing.T) {
	log := filepath.Join(t.TempDir(), "session.log")
	if got := watchdogLines(log); !strings.Contains(got, "no session.log") {
		t.Fatal(got)
	}
	os.WriteFile(log, []byte("== 2026-10-27T10:00:00+00:00 start\n== 2026-10-27T10:01:00+00:00 watchdog: no sign of life for 60 s, restart 1 of 3\n== 2026-10-27T10:01:05+00:00 exit 143\n== 2026-10-27T10:01:06+00:00 start (GPU off)"), 0o644)
	got := watchdogLines(log)
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "restart 1 of 3") {
		t.Fatalf("got %q", got)
	}
	os.WriteFile(log, []byte("== start\n== exit 0\n"), 0o644)
	if got := watchdogLines(log); !strings.Contains(got, "none") {
		t.Fatal(got)
	}
}

func TestWebArgsFollowGPUOff(t *testing.T) {
	t.Setenv("OROS_GPU_OFF", "")
	a := webArgs("/home/oros", "https://duckduckgo.com/")
	if strings.Contains(strings.Join(a, " "), "--disable-gpu") || a[len(a)-1] != "https://duckduckgo.com/" {
		t.Fatalf("got %q", a)
	}
	t.Setenv("OROS_GPU_OFF", "1")
	a = webArgs("/home/oros", "file:///home/oros/Downloads/r.txt")
	if !strings.Contains(strings.Join(a, " "), "--disable-gpu") || a[len(a)-1] != "file:///home/oros/Downloads/r.txt" {
		t.Fatalf("got %q", a)
	}
}

// fakeZoneinfo points the time zone code at a temporary system.
func fakeZoneinfo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	zi := filepath.Join(root, "zoneinfo")
	for _, z := range []string{"UTC", "Europe/Athens", "Europe/Berlin"} {
		p := filepath.Join(zi, z)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("TZif2 fake"), 0o644)
	}
	os.WriteFile(filepath.Join(zi, "Europe", "notes"), []byte("not a zone"), 0o644)
	os.WriteFile(filepath.Join(zi, "zone1970.tab"), []byte("# comment\nGR\t+3758+02343\tEurope/Athens\nDE,DK\t+5230+01322\tEurope/Berlin\tmost of Germany\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	os.MkdirAll(filepath.Join(root, "home"), 0o755)
	old := []string{zoneinfo, localtime, etcZone, zoneFile}
	zoneinfo = zi
	localtime = filepath.Join(root, "etc", "localtime")
	etcZone = filepath.Join(root, "etc", "timezone")
	zoneFile = filepath.Join(root, "home", ".oros-timezone")
	t.Cleanup(func() { zoneinfo, localtime, etcZone, zoneFile = old[0], old[1], old[2], old[3] })
	return root
}

func TestTimeZones(t *testing.T) {
	fakeZoneinfo(t)
	for _, z := range []string{"Europe/Athens", "UTC"} {
		if !validZone(z) {
			t.Fatalf("%s refused", z)
		}
	}
	for _, z := range []string{"", "Europe/Paris", "Europe/notes", "Europe", "../etc/passwd", "Europe/../UTC", "/UTC", "Europe/Athens\n"} {
		if validZone(z) {
			t.Fatalf("%q accepted", z)
		}
	}
	if got := strings.Join(zones(), ","); got != "UTC,Europe/Athens,Europe/Berlin" {
		t.Fatal(got)
	}
	if currentZone() != "UTC" {
		t.Fatal("no /etc/localtime must read as UTC")
	}
	if timeMain([]string{"--apply"}) != 0 {
		t.Fatal("nothing saved must not fail")
	}
	if timeMain([]string{"--zone", "Mars/Olympus"}) == 0 {
		t.Fatal("unknown zone saved")
	}
	if timeMain([]string{"--zone", "Europe/Athens"}) != 0 || currentZone() != "Europe/Athens" || savedZone() != "Europe/Athens" {
		t.Fatalf("set: current %s saved %s", currentZone(), savedZone())
	}
	if b, _ := os.ReadFile(etcZone); string(b) != "Europe/Athens\n" {
		t.Fatalf("/etc/timezone %q", b)
	}
	// A new start: /etc is fresh, the saved zone is applied again.
	os.Remove(localtime)
	if timeMain([]string{"--apply"}) != 0 || currentZone() != "Europe/Athens" {
		t.Fatal("apply at boot")
	}
	os.WriteFile(zoneFile, []byte("../../etc/shadow\n"), 0o644)
	if savedZone() != "" {
		t.Fatal("a broken saved zone must be ignored")
	}
}

func TestParseBrightness(t *testing.T) {
	if v, ok := parseBrightness("intel_backlight,backlight,480,50%,960\n"); !ok || v != 50 {
		t.Fatal(v, ok)
	}
	if v, ok := parseBrightness("input3::capslock,leds,0,0%,1\namdgpu_bl0,backlight,255,100%,255\n"); !ok || v != 100 {
		t.Fatal(v, ok)
	}
	if _, ok := parseBrightness("input3::capslock,leds,0,0%,1\n"); ok {
		t.Fatal("a keyboard LED is not a screen")
	}
	if _, ok := parseBrightness(""); ok {
		t.Fatal("empty")
	}
}

func TestParseBTDevices(t *testing.T) {
	out := "\x1b[0;94m[bluetooth]\x1b[0m# Device AA:BB:CC:DD:EE:01 MX Master 3\r\nDevice AA:BB:CC:DD:EE:02 AA-BB-CC-DD-EE-02\nDevice AA:BB:CC:DD:EE:01 MX Master 3\nDevice nonsense here\n[NEW] Device AA:BB:CC:DD:EE:03 JBL Go 3\n"
	got := parseBTDevices(out, false)
	if len(got) != 2 || got[0].Name != "MX Master 3" || got[1].Mac != "AA:BB:CC:DD:EE:03" {
		t.Fatalf("named: %+v", got)
	}
	if got := parseBTDevices(out, true); len(got) != 3 || got[1].Name != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("all: %+v", got)
	}
}

func TestParsePair(t *testing.T) {
	r := parsePair("Attempting to pair with AA:BB:CC:DD:EE:01\n\x1b[0;93m[CHG]\x1b[0m Device AA:BB:CC:DD:EE:01 Connected: yes\n[agent] Passkey: 042317\n")
	if r["passkey"] != "042317" || r["done"] != nil {
		t.Fatalf("waiting: %v", r)
	}
	r = parsePair("[agent] Passkey: 042317\n[CHG] Device AA:BB:CC:DD:EE:01 Paired: yes\nPairing successful\n")
	if r["done"] != true || r["error"] != nil {
		t.Fatalf("done: %v", r)
	}
	r = parsePair("Attempting to pair with AA:BB:CC:DD:EE:01\nFailed to pair: org.bluez.Error.AuthenticationFailed\n")
	if r["done"] != true || r["error"] != "Failed to pair: org.bluez.Error.AuthenticationFailed" {
		t.Fatalf("failed: %v", r)
	}
	if r := parsePair("Device AA:BB:CC:DD:EE:01 not available\n"); r["error"] == nil {
		t.Fatal("unknown device must fail")
	}
	if r := parsePair("[agent] PIN code: 0000\n"); r["passkey"] != "0000" {
		t.Fatalf("pin: %v", r)
	}
}

func TestBTCommandsCheckAddresses(t *testing.T) {
	for _, mac := range []string{"", "aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF; reboot", "AA:BB:CC:DD:EE"} {
		if r := btPair(mac); r["ok"] != false {
			t.Fatalf("pair %q accepted", mac)
		}
		if r := btDo("forget", mac); r["ok"] != false {
			t.Fatalf("forget %q accepted", mac)
		}
	}
	if r := btDo("reboot", "AA:BB:CC:DD:EE:FF"); r["ok"] != false {
		t.Fatal("unknown action accepted")
	}
}

func TestParsePrinters(t *testing.T) {
	got := parsePrinters("HP_LaserJet_M15w\nBrother_HL_L2350DW_series\n\n", "system default destination: Brother_HL_L2350DW_series\n")
	if len(got) != 2 || got[0].Name != "Brother_HL_L2350DW_series" || !got[0].Default || got[1].Default {
		t.Fatalf("got %+v", got)
	}
	if got := parsePrinters("lpstat: No destinations added.\n", "no system default destination\n"); len(got) != 0 {
		t.Fatalf("none: %+v", got)
	}
	for _, n := range []string{"", "a b", "../x", "x;y"} {
		if printerName.MatchString(n) {
			t.Fatalf("%q accepted", n)
		}
	}
	if r := printTest("x;reboot"); r["ok"] != false {
		t.Fatal("bad printer name accepted")
	}
}

// TestSelftestFiles runs the boot self-test against a stand-in for the
// launcher's computer files API.
func TestSelftestFiles(t *testing.T) {
	var calls []string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "oros_fs", Value: "k", Path: "/__oros/fs/"})
	})
	mux.HandleFunc("/__oros/fs/", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("oros_fs"); err != nil || c.Value != "k" || r.Header.Get("X-Oros-FS") != "1" {
			w.WriteHeader(403)
			return
		}
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/__oros/fs/"))
		switch strings.TrimPrefix(r.URL.Path, "/__oros/fs/") {
		case "disks":
			fmt.Fprint(w, `[{"path":"/dev/sdb1","label":"OROSTEST"}]`)
		case "op":
			fmt.Fprint(w, `{"ok":true,"path":"/disks/OROSTEST"}`)
		case "list":
			fmt.Fprint(w, `[{"name":"hello.txt"}]`)
		case "roots":
			fmt.Fprint(w, `{"disks":[{"path":"/disks/OROSTEST","readonly":false}]}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var log bytes.Buffer
	if err := selftestFiles(&log, srv.URL, "OROSTEST", 5*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	want := "GET disks,POST op,PUT raw,GET list,GET roots,PUT raw,POST op,POST op"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls %s", strings.Join(calls, ","))
	}
	if err := selftestFiles(&log, srv.URL, "OTHER", 5*time.Second); err == nil {
		t.Fatal("found a disk that is not there")
	}
}

func TestReportWithOrosPart(t *testing.T) {
	home := t.TempDir()
	p, err := writeReport(home, "orOS problem report\nversion: 4.67.00")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	i := strings.Index(s, "===== orOS (Settings › Report a problem)\norOS problem report\nversion: 4.67.00\n")
	j := strings.Index(s, "===== this computer\n")
	k := strings.Index(s, "===== uname -a")
	if i < 0 || j < i || k < j {
		t.Fatalf("orOS part missing or out of order: %d %d %d", i, j, k)
	}
}
