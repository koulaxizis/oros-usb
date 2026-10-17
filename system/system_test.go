package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
