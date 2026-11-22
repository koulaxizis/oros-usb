package main

// Graphics cards, wave G2 (2026-11-22, Chris: «να αναγνωρίζει κάρτες
// γραφικών»; his laptop has an Intel GPU plus an older NVIDIA card).
//
// "oros-system gpu" lists the computer's graphics cards from sysfs (vendor,
// model, the kernel driver in use, which one drives the screen) and the
// Vulkan devices that Mesa offers. On a laptop with two cards (the screen
// on Intel or an AMD APU, a second AMD or NVIDIA card for power), "gpu
// --apply" at the start of every session points Flatpak programs (games,
// Steam, Heroic, Lutris, itch) at the strong card through Mesa's DRI_PRIME
// (a global `flatpak override --user`); orOS's own window and the desktop
// stay on the card that drives the screen, which saves the battery. Nothing
// to choose: it follows the hardware at every start. The System panel can
// turn it off ("gpu.offload"), kept in ~/oros/data/gpu.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type gpuCard struct {
	Slot     string `json:"slot"` // PCI address, 0000:01:00.0
	Vendor   string `json:"vendor"`
	VendorID string `json:"vendorId"`
	DeviceID string `json:"deviceId"`
	Name     string `json:"name,omitempty"`
	Driver   string `json:"driver"` // "" when no kernel driver took it
	Boot     bool   `json:"boot"`   // drives the screen at start (boot_vga)
}

type vkDevice struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Driver string `json:"driver"`
}

var gpuVendors = map[string]string{"0x8086": "intel", "0x1002": "amd", "0x10de": "nvidia"}

// sysRoot is "/sys"; tests point it at a fake tree.
var sysRoot = "/sys"

// readGPUs lists the display controllers (PCI class 0x03) behind the DRM
// cards, once per PCI device, the screen's card first.
func readGPUs() []gpuCard {
	cards, _ := filepath.Glob(filepath.Join(sysRoot, "class", "drm", "card*"))
	seen := map[string]bool{}
	var out []gpuCard
	for _, c := range cards {
		base := filepath.Base(c)
		if strings.Contains(base, "-") { // connectors: card0-HDMI-A-1
			continue
		}
		dev := filepath.Join(c, "device")
		real, err := filepath.EvalSymlinks(dev)
		if err != nil {
			continue
		}
		slot := filepath.Base(real)
		if seen[slot] {
			continue
		}
		class := readTrim(filepath.Join(dev, "class"))
		if !strings.HasPrefix(class, "0x03") {
			continue
		}
		seen[slot] = true
		g := gpuCard{
			Slot:     slot,
			VendorID: readTrim(filepath.Join(dev, "vendor")),
			DeviceID: readTrim(filepath.Join(dev, "device")),
			Boot:     readTrim(filepath.Join(dev, "boot_vga")) == "1",
		}
		g.Vendor = gpuVendors[g.VendorID]
		if g.Vendor == "" {
			g.Vendor = "other"
		}
		if d, err := os.Readlink(filepath.Join(dev, "driver")); err == nil {
			g.Driver = filepath.Base(d)
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Boot != out[j].Boot {
			return out[i].Boot
		}
		return out[i].Slot < out[j].Slot
	})
	return out
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// gpuNames fills in the model names from lspci (pci.ids).
func gpuNames(cards []gpuCard) {
	for i := range cards {
		out, err := run("lspci", "-mm", "-s", cards[i].Slot)
		if err != nil {
			continue
		}
		f := lspciFields(out)
		// class, vendor, device, then the subsystem's
		if len(f) >= 3 {
			cards[i].Name = strings.TrimSpace(f[1] + " " + f[2])
		}
	}
}

// lspciFields splits one "lspci -mm" line: quoted fields after the slot.
func lspciFields(line string) []string {
	var f []string
	for {
		i := strings.IndexByte(line, '"')
		if i < 0 {
			return f
		}
		line = line[i+1:]
		j := strings.IndexByte(line, '"')
		if j < 0 {
			return f
		}
		f = append(f, line[:j])
		line = line[j+1:]
	}
}

// renderCard is the card games should use on a computer with two: the
// screen's card is Intel or AMD and another AMD or NVIDIA card is there.
// nil when there is one card, or the screen already runs on the strong
// one (a desktop with its monitor on the graphics card).
func renderCard(cards []gpuCard) *gpuCard {
	var boot *gpuCard
	for i := range cards {
		if cards[i].Boot {
			boot = &cards[i]
			break
		}
	}
	if boot == nil || len(cards) < 2 || boot.Vendor == "nvidia" {
		return nil
	}
	var best *gpuCard
	for i := range cards {
		c := &cards[i]
		if c == boot || c.Driver == "" || c.Driver == "vfio-pci" {
			continue
		}
		if c.Vendor == "nvidia" {
			return c
		}
		if c.Vendor == "amd" && best == nil {
			best = c
		}
	}
	return best
}

// primeEnv is what makes a program render on card c: Mesa's DRI_PRIME
// (OpenGL, and Vulkan through Mesa's device-select layer, also for NVK).
// NVIDIA's own driver (wave G5) uses its PRIME render offload variables.
func primeEnv(c *gpuCard) map[string]string {
	if c == nil {
		return nil
	}
	if c.Driver == "nvidia" {
		return map[string]string{
			"__NV_PRIME_RENDER_OFFLOAD": "1",
			"__GLX_VENDOR_LIBRARY_NAME": "nvidia",
			"__VK_LAYER_NV_optimus":     "NVIDIA_only",
		}
	}
	return map[string]string{"DRI_PRIME": "pci-" + strings.NewReplacer(":", "_", ".", "_").Replace(c.Slot)}
}

// primeKeys are every variable primeEnv may set, so a change of card or
// driver never leaves an old one behind.
var primeKeys = []string{"DRI_PRIME", "__NV_PRIME_RENDER_OFFLOAD", "__GLX_VENDOR_LIBRARY_NAME", "__VK_LAYER_NV_optimus"}

// parseVulkan reads "vulkaninfo --summary": one entry per GPUn block.
func parseVulkan(out string) []vkDevice {
	var l []vkDevice
	var cur *vkDevice
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "GPU") && strings.HasSuffix(t, ":") {
			l = append(l, vkDevice{})
			cur = &l[len(l)-1]
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "deviceName":
			cur.Name = v
		case "deviceType":
			cur.Type = strings.TrimPrefix(v, "PHYSICAL_DEVICE_TYPE_")
		case "driverName":
			cur.Driver = v
		}
	}
	return l
}

type gpuSettings struct {
	// NoOffload: the person turned "games on the strong card" off.
	NoOffload bool `json:"noOffload,omitempty"`
}

func gpuFile(home string) string { return filepath.Join(home, "oros", "data", "gpu.json") }

func readGPUSettings(home string) gpuSettings {
	var s gpuSettings
	if b, err := os.ReadFile(gpuFile(home)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// gpuStatus is "gpu.status" and "oros-system gpu".
func gpuStatus() reply {
	home, _ := os.UserHomeDir()
	cards := readGPUs()
	gpuNames(cards)
	r := reply{"ok": true, "cards": cards}
	out, _ := runFor(15*time.Second, "vulkaninfo", "--summary")
	r["vulkan"] = parseVulkan(out)
	if c := renderCard(cards); c != nil {
		r["hybrid"] = true
		r["gameCard"] = c.Slot
		r["offload"] = !readGPUSettings(home).NoOffload
	}
	return r
}

// gpuApply sets or clears the Flatpak override; run at every session start
// (oros-session) and after "gpu.offload".
func gpuApply() error {
	home, _ := os.UserHomeDir()
	env := primeEnv(renderCard(readGPUs()))
	if readGPUSettings(home).NoOffload {
		env = nil
	}
	args := []string{"override", "--user"}
	for _, k := range primeKeys {
		if v, ok := env[k]; ok {
			args = append(args, "--env="+k+"="+v)
		} else {
			args = append(args, "--unset-env="+k)
		}
	}
	if out, err := run("flatpak", args...); err != nil {
		return fmt.Errorf("flatpak override: %v %s", err, strings.TrimSpace(out))
	}
	if env != nil {
		logf("gpu: games on the second card %v", env)
	}
	return nil
}

// gpuSetOffload is "gpu.offload" (on: games on the strong card, default).
func gpuSetOffload(on bool) reply {
	home, _ := os.UserHomeDir()
	b, _ := json.Marshal(gpuSettings{NoOffload: !on})
	if err := os.MkdirAll(filepath.Dir(gpuFile(home)), 0o755); err != nil {
		return reply{"ok": false, "error": err.Error()}
	}
	if err := os.WriteFile(gpuFile(home), b, 0o644); err != nil {
		return reply{"ok": false, "error": err.Error()}
	}
	if err := gpuApply(); err != nil {
		return reply{"ok": false, "error": err.Error()}
	}
	return gpuStatus()
}

// gpuMain is "oros-system gpu [--apply]".
func gpuMain(args []string) int {
	if len(args) == 1 && args[0] == "--apply" {
		if err := gpuApply(); err != nil {
			logf("gpu: %v", err)
			return 1
		}
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: oros-system gpu [--apply]")
		return 2
	}
	b, _ := json.MarshalIndent(gpuStatus(), "", "  ")
	fmt.Println(string(b))
	return 0
}

// selftestGPU (boot test, inside the session): the card QEMU shows is
// found with its driver, and the Flatpak override was written.
func selftestGPU(check func(string, error), info func(string, string)) {
	cards := readGPUs()
	var err error
	if len(cards) == 0 {
		err = fmt.Errorf("no graphics card in /sys/class/drm")
	} else if cards[0].Driver == "" {
		err = fmt.Errorf("%s has no driver", cards[0].Slot)
	}
	var names []string
	for _, c := range cards {
		names = append(names, c.Vendor+" "+c.VendorID+":"+c.DeviceID+" "+c.Driver)
	}
	check("graphics cards "+strings.Join(names, ", "), err)
	check("games on the right card (flatpak override)", gpuApply())
	out, _ := run("flatpak", "override", "--user", "--show")
	info("flatpak override", strings.ReplaceAll(strings.TrimSpace(out), "\n", " "))
	info("second card for games", fmt.Sprint(renderCard(cards) != nil))
}
