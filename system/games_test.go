package main

import (
	"strings"
	"testing"
)

func TestInhibitors(t *testing.T) {
	h := newInhibitors()
	if on, _ := h.held(); on {
		t.Fatal("held at start")
	}
	a := h.add(":1.5", "Steam", "game")
	b := h.add(":1.6", "gamemode", "x")
	if a == 0 || b == 0 || a == b {
		t.Fatalf("cookies %d %d", a, b)
	}
	// another caller cannot give back someone else's hold
	h.remove(":1.6", a)
	if on, _ := h.held(); !on {
		t.Fatal("hold dropped by the wrong caller")
	}
	h.remove(":1.5", a)
	if on, app := h.held(); !on || app != "gamemode" {
		t.Fatalf("held %v %q", on, app)
	}
	// a caller that leaves the bus loses its holds
	h.add(":1.6", "gamemode", "y")
	h.gone(":1.6")
	if on, _ := h.held(); on {
		t.Fatal("holds of a gone caller kept")
	}
}

func TestInhibitorsBounded(t *testing.T) {
	h := newInhibitors()
	for i := 0; i < 500; i++ {
		h.add(":1.9", "loop", "")
	}
	if len(h.byCook) > 64 {
		t.Fatalf("%d holds", len(h.byCook))
	}
	in := h.byCook[h.add(":1.9", "a\nb\x00"+strings.Repeat("z", 200), "")]
	if strings.ContainsAny(in.app, "\n\x00") || len(in.app) > 80 {
		t.Fatalf("app %q", in.app)
	}
}
