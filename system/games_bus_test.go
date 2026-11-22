package main

import (
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// Runs only on a session bus (dbus-run-session go test ./...): the real
// service answers Inhibit and UnInhibit, and a caller that leaves the
// bus loses its hold.
func TestScreenSaverOnBus(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no session bus")
	}
	h := serveScreenSaver()
	if h == nil {
		t.Fatal("service did not start")
	}
	c, err := dbus.ConnectSessionBus(dbus.WithSignalHandler(dbus.NewSequentialSignalHandler()))
	if err != nil {
		t.Fatal(err)
	}
	// a second, private connection, closed to look like a crashed game
	c2, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err := c2.Hello(); err != nil {
		t.Fatal(err)
	}
	var k uint32
	if err := c.Object(ssName, "/org/freedesktop/ScreenSaver").Call(ssIface+".Inhibit", 0, "Game", "playing").Store(&k); err != nil || k == 0 {
		t.Fatalf("inhibit %d %v", k, err)
	}
	if on, app := h.held(); !on || app != "Game" {
		t.Fatalf("held %v %q", on, app)
	}
	if err := c.Object(ssName, "/ScreenSaver").Call(ssIface+".UnInhibit", 0, k).Err; err != nil {
		t.Fatal(err)
	}
	if on, _ := h.held(); on {
		t.Fatal("still held after UnInhibit")
	}
	var k2 uint32
	if err := c2.Object(ssName, "/org/freedesktop/ScreenSaver").Call(ssIface+".Inhibit", 0, "Crashy", "").Store(&k2); err != nil {
		t.Fatal(err)
	}
	c2.Close()
	for i := 0; i < 50; i++ {
		if on, _ := h.held(); !on {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a gone caller kept its hold")
}
