package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeskDefaultsAndJunk(t *testing.T) {
	home := t.TempDir()
	if d := readDesk(home); d.ScreenOff != 10 || d.Sleep != 0 || d.xkbLayouts() != "us,gr" || d.xkbOptions() != "grp:alt_shift_toggle" || d.Reader {
		t.Fatalf("defaults: %+v", d)
	}
	os.MkdirAll(filepath.Join(home, "oros", "data"), 0o755)
	// Every unknown value falls back on its own; known ones stay.
	os.WriteFile(deskFile(home), []byte(`{"screenOff":7,"sleep":30,"layouts":["de","$(reboot)","de","us","fr","it","es"],"switch":"x;rm","reader":true,"readerLang":"../x"}`), 0o644)
	d := readDesk(home)
	if d.ScreenOff != 10 || d.Sleep != 30 || d.xkbLayouts() != "de,us,fr,it" || d.Switch != "alt_shift" || !d.Reader || d.ReaderLang != "" {
		t.Fatalf("sanitized: %+v", d)
	}
	os.WriteFile(deskFile(home), []byte(`not json`), 0o644)
	if d := readDesk(home); d.xkbLayouts() != "us,gr" {
		t.Fatalf("broken file: %+v", d)
	}
	os.WriteFile(deskFile(home), []byte(`{"layouts":["zz"]}`), 0o644)
	if d := readDesk(home); d.xkbLayouts() != "us,gr" {
		t.Fatalf("no known layout: %+v", d)
	}
}

func TestDeskOneLayoutHasNoSwitch(t *testing.T) {
	d := deskSettings{Layouts: []string{"de"}, Switch: "win_space"}.sanitize()
	if d.xkbLayouts() != "de" || d.xkbOptions() != "" {
		t.Fatalf("%q %q", d.xkbLayouts(), d.xkbOptions())
	}
	d.Layouts = []string{"de", "gr"}
	if d.xkbOptions() != "grp:win_space_toggle" {
		t.Fatalf("%q", d.xkbOptions())
	}
}

func TestDeskSetKeepsUnsentFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, "oros", "data"), 0o755)
	if r := deskSet(json.RawMessage(`{"layouts":["gr","de"],"switch":"ctrl_shift"}`)); r["ok"] != true {
		t.Fatalf("%v", r)
	}
	if r := deskSet(json.RawMessage(`{"screenOff":0}`)); r["ok"] != true {
		t.Fatalf("%v", r)
	}
	d := readDesk(home)
	if d.ScreenOff != 0 || d.xkbLayouts() != "gr,de" || d.Switch != "ctrl_shift" {
		t.Fatalf("%+v", d)
	}
	if r := deskSet(json.RawMessage(`[1]`)); r["ok"] != false {
		t.Fatalf("bad settings saved: %v", r)
	}
}

func TestIdleArgs(t *testing.T) {
	if a := idleArgs("/x", deskSettings{}); a != nil {
		t.Fatalf("never: %q", a)
	}
	a := strings.Join(idleArgs("/x", deskSettings{ScreenOff: 5, Sleep: 30}), "|")
	want := "timeout|300|/x screen --off|resume|/x screen --on|timeout|1800|systemctl suspend|after-resume|/x screen --on"
	if a != want {
		t.Fatalf("got %s", a)
	}
	a = strings.Join(idleArgs("/x", deskSettings{Sleep: 15}), "|")
	if a != "timeout|900|systemctl suspend" {
		t.Fatalf("sleep only: %s", a)
	}
}

func TestReaderEnv(t *testing.T) {
	old := localeExists
	defer func() { localeExists = old }()
	localeExists = func(string) bool { return true }
	env := readerEnv([]string{"PATH=/bin", "LANG=C.UTF-8", "LC_ALL=C"}, "el")
	if strings.Join(env, " ") != "PATH=/bin LANG=el_GR.UTF-8 LANGUAGE=el_GR" {
		t.Fatalf("%q", env)
	}
	if env := readerEnv([]string{"LANG=C.UTF-8"}, "xx"); strings.Join(env, " ") != "LANG=C.UTF-8" {
		t.Fatalf("unknown language: %q", env)
	}
	localeExists = func(string) bool { return false }
	if env := readerEnv([]string{"LANG=C.UTF-8"}, "de"); strings.Join(env, " ") != "LANG=C.UTF-8" {
		t.Fatalf("missing locale: %q", env)
	}
}

func TestScreenshotNamesAndUsers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := filepath.Join(home, "oros", "data")
	os.MkdirAll(data, 0o755)
	bin := filepath.Join(t.TempDir(), "grim")
	os.WriteFile(bin, []byte("#!/bin/sh\nfor a; do last=\"$a\"; done\nprintf '\\211PNG fake' > \"$last\"\n"), 0o755)
	old := grimBin
	grimBin = bin
	defer func() { grimBin = old }()
	r1, r2 := screenshot(), screenshot()
	if r1["ok"] != true || r2["ok"] != true || r1["folder"] != "/home/Pictures/Screenshots" {
		t.Fatalf("%v %v", r1, r2)
	}
	f1, f2 := r1["file"].(string), r2["file"].(string)
	if f1 == f2 || !strings.HasPrefix(f1, "Screenshot ") || !strings.HasSuffix(f2, ".png") {
		t.Fatalf("%q %q", f1, f2)
	}
	if _, err := os.Stat(filepath.Join(home, "Pictures", "Screenshots", f2)); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(home, "Pictures", "Screenshots", ".shot-*"))
	if len(left) != 0 {
		t.Fatalf("temporary file left: %v", left)
	}
	// A second user's screenshots go to that user's own home.
	os.WriteFile(filepath.Join(data, "users.json"), []byte(`{"users":[{"id":"u00000001","owner":true},{"id":"u00000002","dir":"users/u00000002"}]}`), 0o644)
	os.MkdirAll(filepath.Join(data, "users"), 0o755)
	os.WriteFile(filepath.Join(data, "users", "current.json"), []byte(`{"id":"u00000002"}`), 0o644)
	if r := screenshot(); r["ok"] != true {
		t.Fatalf("%v", r)
	} else if _, err := os.Stat(filepath.Join(home, "oros", "homes", "u00000002", "Pictures", "Screenshots", r["file"].(string))); err != nil {
		t.Fatal(err)
	}
	// grim failing leaves nothing behind.
	os.WriteFile(bin, []byte("#!/bin/sh\necho 'compositor does not support screencopy' >&2\nexit 1\n"), 0o755)
	if r := screenshot(); r["ok"] != false || !strings.Contains(r["error"].(string), "screencopy") {
		t.Fatalf("%v", r)
	}
}

func TestAwakeHold(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	now := time.Now()
	if awakeHeld(now) {
		t.Fatal("held without a ping")
	}
	if r := handle(request{Cmd: "ping", Awake: true}); r["ok"] != true {
		t.Fatalf("ping: %v", r)
	}
	if !awakeHeld(time.Now()) {
		t.Fatal("a ping with awake should hold")
	}
	if awakeHeld(time.Now().Add(awakeFresh + time.Second)) {
		t.Fatal("a hold not renewed must end")
	}
	handle(request{Cmd: "ping"})
	if awakeHeld(time.Now()) {
		t.Fatal("a ping without awake ends the hold")
	}
}
