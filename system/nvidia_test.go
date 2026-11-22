package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNvidiaPinSupports(t *testing.T) {
	p := nvidiaPin{DevIDs: []string{"0x1C8D", "0x1340"}}
	if !p.supports(gpuCard{Vendor: "nvidia", DeviceID: "0x1c8d"}) {
		t.Fatal("GTX 1050 Mobile not supported")
	}
	if p.supports(gpuCard{Vendor: "nvidia", DeviceID: "0x1292"}) {
		t.Fatal("a Kepler card (470 branch) taken")
	}
	if p.supports(gpuCard{Vendor: "amd", DeviceID: "0x1c8d"}) {
		t.Fatal("another vendor's id taken")
	}
	cards := []gpuCard{{Vendor: "intel", DeviceID: "0x1616", Boot: true}, {Slot: "0000:01:00.0", Vendor: "nvidia", DeviceID: "0x1340"}}
	if c := nvidiaCardFor(p, cards); c == nil || c.Slot != "0000:01:00.0" {
		t.Fatalf("card %+v", c)
	}
}

func TestParseMokTest(t *testing.T) {
	for out, want := range map[string]string{
		"/opt/oros/oros-mok.der is already enrolled\n":                  "enrolled",
		"/opt/oros/oros-mok.der is already in the enrollment request\n": "pending",
		"/opt/oros/oros-mok.der is not enrolled\n":                      "",
		"EFI variables are not supported on this system\n":              "",
	} {
		if got := parseMokTest(out); got != want {
			t.Fatalf("%q: %q, want %q", out, got, want)
		}
	}
	for i := 0; i < 20; i++ {
		c, err := mokCode()
		if err != nil || len(c) != 8 || strings.Trim(c, "0123456789") != "" {
			t.Fatalf("code %q %v", c, err)
		}
	}
}

func TestBootArgsDropsNvidiaOptions(t *testing.T) {
	got := bootArgs("BOOT_IMAGE=/x boot=live quiet "+nvidiaArgs+" persistence live-media-path=/oros-system/A", "u", "B")
	for _, a := range strings.Fields(nvidiaArgs) {
		if strings.Contains(got, a) {
			t.Fatalf("%q kept in %q", a, got)
		}
	}
	if strings.Count(got, "${oros_gpu}") != 1 {
		t.Fatalf("GRUB's ${oros_gpu} missing or doubled: %q", got)
	}
	// an entry already written with the variable keeps it once
	if g := bootArgs(got, "u", "A"); strings.Count(g, "${oros_gpu}") != 1 {
		t.Fatalf("%q", g)
	}
}

func TestWriteGrubGPU(t *testing.T) {
	target := t.TempDir()
	if err := writeGrubGPU(target, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(target, "oros-system", "gpu.cfg"))
	if err != nil || !strings.Contains(string(b), `set oros_gpu="`+nvidiaArgs+`"`) {
		t.Fatalf("gpu.cfg %v %q", err, b)
	}
	if err := writeGrubGPU(target, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "oros-system", "gpu.cfg")); !os.IsNotExist(err) {
		t.Fatal("gpu.cfg kept")
	}
	if err := writeGrubGPU(target, false); err != nil {
		t.Fatal("removing twice fails:", err)
	}
}

func TestFetchPackChecks(t *testing.T) {
	dir := t.TempDir()
	old := nvidiaDir
	nvidiaDir = dir
	defer func() { nvidiaDir = old }()
	src := filepath.Join(t.TempDir(), "p.raw")
	data := []byte("not really a squashfs, but bytes with a known sum")
	_ = os.WriteFile(src, data, 0o644)
	pin := nvidiaPin{File: "orOS-USB-1-nvidia.raw", Size: int64(len(data)), SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}
	dst := filepath.Join(dir, pin.File)
	if err := fetchPack(src, pin, dst, func(int64) {}); err == nil {
		t.Fatal("a pack with the wrong checksum was kept")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("damaged pack left behind")
	}
	pin.SHA256 = sha256hex(data)
	if err := fetchPack(src, pin, dst, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if !packOK(dst, pin.Size) {
		t.Fatal("pack not there")
	}
	// only the official release address is fetched
	pin.URL = "https://example.com/x.raw"
	if err := fetchPack("", pin, dst+"2", func(int64) {}); err == nil {
		t.Fatal("another address fetched")
	}
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
