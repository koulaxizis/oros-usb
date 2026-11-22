package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeCard makes /sys/class/drm/<card> -> devices/<slot> with the files
// readGPUs reads.
func fakeCard(t *testing.T, root, card, slot, vendor, device, class, driver string, boot bool) {
	t.Helper()
	dev := filepath.Join(root, "devices", slot)
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(n, v string) { _ = os.WriteFile(filepath.Join(dev, n), []byte(v+"\n"), 0o644) }
	w("vendor", vendor)
	w("device", device)
	w("class", class)
	if boot {
		w("boot_vga", "1")
	} else {
		w("boot_vga", "0")
	}
	if driver != "" {
		drv := filepath.Join(root, "drivers", driver)
		_ = os.MkdirAll(drv, 0o755)
		_ = os.Symlink(drv, filepath.Join(dev, "driver"))
	}
	cd := filepath.Join(root, "class", "drm", card)
	_ = os.MkdirAll(cd, 0o755)
	_ = os.Symlink(dev, filepath.Join(cd, "device"))
}

func withSys(t *testing.T) string {
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	t.Cleanup(func() { sysRoot = old })
	return root
}

func TestHybridIntelNvidia(t *testing.T) {
	root := withSys(t)
	// the old Lenovo: Intel drives the screen, an NVIDIA card on nouveau
	fakeCard(t, root, "card1", "0000:01:00.0", "0x10de", "0x1299", "0x030200", "nouveau", false)
	fakeCard(t, root, "card0", "0000:00:02.0", "0x8086", "0x1616", "0x030000", "i915", true)
	fakeCard(t, root, "card0-eDP-1", "0000:00:02.0", "0x8086", "0x1616", "0x030000", "i915", true)
	cards := readGPUs()
	if len(cards) != 2 || cards[0].Vendor != "intel" || !cards[0].Boot {
		t.Fatalf("cards %+v", cards)
	}
	c := renderCard(cards)
	if c == nil || c.Vendor != "nvidia" {
		t.Fatalf("render card %+v", c)
	}
	env := primeEnv(c)
	if env["DRI_PRIME"] != "pci-0000_01_00_0" || len(env) != 1 {
		t.Fatalf("env %v", env)
	}
	// with NVIDIA's own driver (G5): its PRIME offload variables
	c.Driver = "nvidia"
	env = primeEnv(c)
	if env["__NV_PRIME_RENDER_OFFLOAD"] != "1" || env["DRI_PRIME"] != "" {
		t.Fatalf("env %v", env)
	}
	for k := range env {
		found := false
		for _, p := range primeKeys {
			found = found || p == k
		}
		if !found {
			t.Fatalf("%s not in primeKeys (would never be cleared)", k)
		}
	}
}

func TestOneCardOrScreenOnStrongCard(t *testing.T) {
	root := withSys(t)
	fakeCard(t, root, "card0", "0000:00:02.0", "0x8086", "0x9a49", "0x030000", "i915", true)
	if renderCard(readGPUs()) != nil {
		t.Fatal("one card: nothing to offload")
	}
	// a desktop: monitor on the NVIDIA card, the CPU's graphics also on
	root = withSys(t)
	fakeCard(t, root, "card0", "0000:01:00.0", "0x10de", "0x2484", "0x030000", "nouveau", true)
	fakeCard(t, root, "card1", "0000:00:02.0", "0x8086", "0x4680", "0x030000", "i915", false)
	if c := renderCard(readGPUs()); c != nil {
		t.Fatalf("screen already on the strong card, got %+v", c)
	}
	// a second card no driver took, or one passed to a VM, is never used
	root = withSys(t)
	fakeCard(t, root, "card0", "0000:00:02.0", "0x8086", "0x1616", "0x030000", "i915", true)
	fakeCard(t, root, "card1", "0000:01:00.0", "0x1002", "0x6900", "0x038000", "vfio-pci", false)
	if c := renderCard(readGPUs()); c != nil {
		t.Fatalf("vfio card used: %+v", c)
	}
}

func TestAMDAPUWithAMDCard(t *testing.T) {
	root := withSys(t)
	fakeCard(t, root, "card0", "0000:05:00.0", "0x1002", "0x1638", "0x030000", "amdgpu", true)
	fakeCard(t, root, "card1", "0000:03:00.0", "0x1002", "0x73ff", "0x030000", "amdgpu", false)
	// a non-display PCI device behind a DRM node is skipped
	fakeCard(t, root, "card2", "0000:07:00.0", "0x1af4", "0x1050", "0x058000", "virtio", false)
	cards := readGPUs()
	if len(cards) != 2 {
		t.Fatalf("cards %+v", cards)
	}
	c := renderCard(cards)
	if c == nil || c.Slot != "0000:03:00.0" {
		t.Fatalf("render card %+v", c)
	}
}

func TestParseVulkanAndLspci(t *testing.T) {
	out := `==========
VULKANINFO
==========
Devices:
========
GPU0:
	apiVersion         = 1.4.305
	deviceType         = PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU
	deviceName         = Intel(R) HD Graphics 5500 (BDW GT2)
	driverName         = Intel open-source Mesa driver
GPU1:
	deviceType         = PHYSICAL_DEVICE_TYPE_CPU
	deviceName         = llvmpipe (LLVM 19.1.7, 256 bits)
	driverName         = llvmpipe
`
	l := parseVulkan(out)
	if len(l) != 2 || l[0].Type != "INTEGRATED_GPU" || l[1].Driver != "llvmpipe" || l[0].Name != "Intel(R) HD Graphics 5500 (BDW GT2)" {
		t.Fatalf("%+v", l)
	}
	f := lspciFields(`01:00.0 "3D controller" "NVIDIA Corporation" "GM108M [GeForce 940M]" -ra2 "Lenovo" "Device 3815"`)
	if len(f) < 4 || f[1] != "NVIDIA Corporation" || f[2] != "GM108M [GeForce 940M]" {
		t.Fatalf("%q", f)
	}
}
