package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
