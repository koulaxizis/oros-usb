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
	old := webBrowser
	webBrowser = stub
	defer func() { webBrowser = old }()
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
	if d := mountDir(volume{Path: "/dev/sdc1", Label: "My Disk/../x"}); d != filepath.Join(mediaRoot, "My_Disk_.._x") {
		t.Fatalf("dir %q", d)
	}
	if d := mountDir(volume{Path: "/dev/sdc1", Label: ".."}); d != filepath.Join(mediaRoot, "sdc1") {
		t.Fatalf("dir %q", d)
	}
}

// TestMountVolume mounts real filesystems read-only (root and loop devices
// only, as in CI).
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
	for fs, mkfs := range map[string][]string{
		"vfat": {"mkfs.vfat", "-n", "STICK"},
		"ext4": {"mkfs.ext4", "-q", "-L", "LINUX"},
		"ntfs": {"mkntfs", "-Q", "-q", "-F", "-L", "Windows"},
	} {
		if _, err := exec.LookPath(mkfs[0]); err != nil {
			t.Logf("%s: no %s", fs, mkfs[0])
			continue
		}
		if b, _ := os.ReadFile("/proc/filesystems"); fs != "ntfs" && !strings.Contains(string(b), "\t"+fs+"\n") {
			if exec.Command("modprobe", fs).Run() != nil {
				t.Logf("%s: not in this kernel", fs)
				continue
			}
		}
		img := filepath.Join(t.TempDir(), fs+".img")
		if err := os.WriteFile(img, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(img, 64<<20); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(mkfs[0], append(mkfs[1:], img)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", fs, err, out)
		}
		out, err := exec.Command("losetup", "-f", "--show", img).Output()
		if err != nil {
			t.Skipf("losetup: %v", err)
		}
		dev := strings.TrimSpace(string(out))
		v := volume{Path: dev, Label: "Disk-" + fs, FSType: fs}
		dir, err := mountVolume(v, "1000", "1000")
		if err != nil {
			_ = exec.Command("losetup", "-d", dev).Run()
			t.Fatalf("%s: %v", fs, err)
		}
		if dir != filepath.Join(mediaRoot, "Disk-"+fs) {
			t.Errorf("%s: dir %s", fs, dir)
		}
		if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err == nil {
			t.Errorf("%s: mounted writable", fs)
		}
		v.Mount = dir
		if err := unmountVolume(v); err != nil {
			t.Errorf("%s: %v", fs, err)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s: mount folder left", fs)
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
	p, err := writeReport(home)
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
