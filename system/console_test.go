package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestConsoleCommands(t *testing.T) {
	g := consoleGamescope()
	if strings.Join(g, " ") != "-e -- flatpak run com.valvesoftware.Steam -gamepadui" {
		t.Fatalf("gamescope %q", g)
	}
	l := consoleLabwc()
	if len(l) != 4 || l[0] != "-C" || l[2] != "-S" {
		t.Fatalf("labwc %q", l)
	}
	// labwc runs -S through sh: the quoted command gives back the same words
	out, err := exec.Command("/bin/sh", "-c", "printf '%s\\n' "+l[3]).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(out)); strings.Join(got, " ") != strings.Join(consoleSteam(), " ") {
		t.Fatalf("labwc runs %q", got)
	}
	if shellQuote("a'b") != `'a'\''b'` {
		t.Fatal(shellQuote("a'b"))
	}
}

func TestConsoleSettings(t *testing.T) {
	home := t.TempDir()
	if readConsole(home).Boot {
		t.Fatal("console mode at start by default")
	}
	if err := writeConsole(home, consoleSettings{Boot: true}); err != nil {
		t.Fatal(err)
	}
	if !readConsole(home).Boot {
		t.Fatal("not kept")
	}
	if err := os.WriteFile(consoleFile(home), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if readConsole(home).Boot {
		t.Fatal("a broken file starts the console")
	}
}

func TestConsoleUsage(t *testing.T) {
	for _, a := range [][]string{nil, {"--run", "--boot-wanted"}, {"--x"}} {
		if consoleMain(a) != 2 {
			t.Errorf("%q accepted", a)
		}
	}
}
