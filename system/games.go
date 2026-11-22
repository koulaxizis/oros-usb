package main

// Games, wave G1 (2026-11-22, Chris: «υποστήριξη για Windows Gaming όσο πιο
// καλά ρυθμισμένο γίνεται»). The image carries the base that Steam, Heroic,
// itch and Lutris (installed later through the software center, Flatpak
// from Flathub) need on the system side: Vulkan, gamemode, Valve's udev
// rules for controllers, Flatpak with its portals, and kernel settings.
//
// This file adds the one piece a kiosk lacks: an org.freedesktop.ScreenSaver
// service on the session bus. Games (SDL), Steam, gamemode and the portals
// ask it not to let the screen go off; while anyone holds it, oros-system
// idle keeps swayidle stopped, the same way orOS's Keep awake does. Without
// it the screen went off in the middle of a game played with a controller.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	ssName  = "org.freedesktop.ScreenSaver"
	ssIface = "org.freedesktop.ScreenSaver"
)

// inhibitors is the bookkeeping behind the service: one cookie per Inhibit
// call, dropped by UnInhibit or when the caller leaves the bus (a game that
// crashed never keeps the screen on).
type inhibitors struct {
	mu     sync.Mutex
	next   uint32
	byCook map[uint32]inhibitor
}

type inhibitor struct {
	sender, app, reason string
}

func newInhibitors() *inhibitors { return &inhibitors{byCook: map[uint32]inhibitor{}} }

func (h *inhibitors) add(sender, app, reason string) uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	// at most 64 holds: a caller in a loop cannot grow this without end
	if len(h.byCook) >= 64 {
		for c := range h.byCook {
			delete(h.byCook, c)
			break
		}
	}
	h.next++
	if h.next == 0 {
		h.next = 1
	}
	h.byCook[h.next] = inhibitor{sender, trimField(app), trimField(reason)}
	return h.next
}

// remove drops a cookie, only for the caller that made it.
func (h *inhibitors) remove(sender string, cookie uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if in, ok := h.byCook[cookie]; ok && in.sender == sender {
		delete(h.byCook, cookie)
	}
}

// gone drops every cookie of a caller that left the bus.
func (h *inhibitors) gone(sender string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c, in := range h.byCook {
		if in.sender == sender {
			delete(h.byCook, c)
		}
	}
}

func (h *inhibitors) held() (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, in := range h.byCook {
		return true, in.app
	}
	return false, ""
}

func trimField(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// screenSaver is the D-Bus object (both paths that callers use).
type screenSaver struct{ h *inhibitors }

func (s screenSaver) Inhibit(sender dbus.Sender, app, reason string) (uint32, *dbus.Error) {
	return s.h.add(string(sender), app, reason), nil
}

func (s screenSaver) UnInhibit(sender dbus.Sender, cookie uint32) *dbus.Error {
	s.h.remove(string(sender), cookie)
	return nil
}

// The rest of the interface, so callers that ask do not get errors.
func (s screenSaver) GetActive() (bool, *dbus.Error)            { return false, nil }
func (s screenSaver) SimulateUserActivity() *dbus.Error         { return nil }
func (s screenSaver) GetActiveTime() (uint32, *dbus.Error)      { return 0, nil }
func (s screenSaver) GetSessionIdleTime() (uint32, *dbus.Error) { return 0, nil }
func (s screenSaver) SetActive(bool) (bool, *dbus.Error)        { return false, nil }
func (s screenSaver) Lock() *dbus.Error                         { return nil }
func (s screenSaver) Throttle(dbus.Sender, string, string) (uint32, *dbus.Error) {
	return 0, nil
}
func (s screenSaver) UnThrottle(dbus.Sender, uint32) *dbus.Error { return nil }

// serveScreenSaver owns org.freedesktop.ScreenSaver on the session bus for
// as long as oros-system idle runs. It returns the holds (nil when the bus
// or the name is not there: then nothing changes for idle).
func serveScreenSaver() *inhibitors {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		logf("screensaver: no session bus: %v", err)
		return nil
	}
	h := newInhibitors()
	obj := screenSaver{h}
	for _, p := range []dbus.ObjectPath{"/org/freedesktop/ScreenSaver", "/ScreenSaver"} {
		if err := conn.Export(obj, p, ssIface); err != nil {
			logf("screensaver: export %s: %v", p, err)
			conn.Close()
			return nil
		}
	}
	r, err := conn.RequestName(ssName, dbus.NameFlagDoNotQueue)
	if err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		logf("screensaver: name not ours (%v, %v)", r, err)
		conn.Close()
		return nil
	}
	// A caller that leaves the bus loses its holds.
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err == nil {
		ch := make(chan *dbus.Signal, 16)
		conn.Signal(ch)
		go func() {
			for sig := range ch {
				if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) != 3 {
					continue
				}
				name, _ := sig.Body[0].(string)
				newOwner, _ := sig.Body[2].(string)
				if strings.HasPrefix(name, ":") && newOwner == "" {
					h.gone(name)
				}
			}
		}()
	}
	return h
}

// ---------- boot self-test (inside labwc, from selftestDeskMain) ----------

// selftestGames checks the system side of games: Vulkan finds a device
// (in QEMU Mesa's software lavapipe), Flatpak and the portals answer,
// gamemode answers, controllers can make virtual devices (/dev/uinput for
// Steam Input), the kernel settings are in place, and a hold through
// org.freedesktop.ScreenSaver is taken and given back.
func selftestGames(check func(string, error), info func(string, string)) {
	out, err := run("vulkaninfo", "--summary")
	if err == nil && !strings.Contains(out, "deviceName") {
		err = fmt.Errorf("no Vulkan device")
	}
	var devs []string
	for _, l := range strings.Split(out, "\n") {
		if _, name, ok := strings.Cut(l, "deviceName"); ok {
			devs = append(devs, strings.TrimSpace(strings.TrimLeft(name, " =")))
		}
	}
	check("vulkan "+strings.Join(devs, ", "), err)

	// Console mode uses gamescope when the system has it (Debian 13 does
	// not ship it); otherwise Big Picture runs in labwc.
	if _, err := exec.LookPath("gamescope"); err == nil {
		info("console mode", "gamescope")
	} else {
		info("console mode", "labwc (no gamescope)")
	}

	out, err = run("flatpak", "--version")
	check("flatpak "+strings.TrimSpace(out), err)

	out, err = run("gamemoded", "-s")
	if err == nil && !strings.Contains(out, "gamemode is") {
		err = fmt.Errorf("%q", strings.TrimSpace(out))
	}
	check("gamemode "+strings.TrimSpace(out), err)

	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err == nil {
		f.Close()
	}
	check("controllers (/dev/uinput for Steam Input)", err)

	b, err := os.ReadFile("/proc/sys/vm/max_map_count")
	if err == nil && strings.TrimSpace(string(b)) != "2147483642" {
		err = fmt.Errorf("vm.max_map_count is %s", strings.TrimSpace(string(b)))
	}
	check("vm.max_map_count", err)

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		check("session bus", err)
		return
	}
	defer conn.Close()
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		Call("org.freedesktop.DBus.Peer.Ping", 0).Err
	check("portals (xdg-desktop-portal)", err)

	// the idle loop polls every 5 s: hold, see it held, give it back
	var cookie uint32
	ss := conn.Object(ssName, "/org/freedesktop/ScreenSaver")
	err = ss.Call(ssIface+".Inhibit", 0, "oros-selftest", "boot test").Store(&cookie)
	if err == nil && cookie == 0 {
		err = fmt.Errorf("cookie 0")
	}
	if err == nil {
		err = ss.Call(ssIface+".UnInhibit", 0, cookie).Err
	}
	check("screen stays on during games (org.freedesktop.ScreenSaver)", err)
	out, _ = run("sysctl", "-n", "kernel.split_lock_mitigate")
	info("kernel.split_lock_mitigate", out)
}
