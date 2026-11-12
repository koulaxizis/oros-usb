package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeFprint(t *testing.T, list, verify string, verr error) *[]string {
	t.Helper()
	oldRun, oldLook, oldPath := fprintRun, fprintLook, bioPathFn
	t.Cleanup(func() { fprintRun, fprintLook, bioPathFn = oldRun, oldLook, oldPath })
	dir := t.TempDir()
	bioPathFn = func() string { return filepath.Join(dir, "oros", "lock-bio.json") }
	fprintLook = func(string) (string, error) { return "/usr/bin/x", nil }
	var ran []string
	fprintRun = func(ctx context.Context, name string, args ...string) (string, error) {
		ran = append(ran, name)
		if name == "fprintd-list" {
			return list, nil
		}
		return verify, verr
	}
	return &ran
}

const kidA = "0123456789abcdef01234567"

func TestBioFingerprint(t *testing.T) {
	ran := fakeFprint(t, "Fingerprints for user oros on Goodix:\n - #0: right-index-finger\n", "Verify result: verify-match (done)\n", nil)
	st := handle(request{Cmd: "bio.state"})
	if st["can"] != "ok" || st["kind"] != "finger" {
		t.Fatalf("state %v", st)
	}
	r := handle(request{Cmd: "bio.seal", Kid: kidA})
	if r["ok"] != true || len(r["d"].(string)) != 44 {
		t.Fatalf("seal %v", r)
	}
	if fi, err := os.Stat(bioPathFn()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file %v %v", fi, err)
	}
	if o := handle(request{Cmd: "bio.open", Kid: kidA}); o["d"] != r["d"] {
		t.Errorf("open %v", o)
	}
	if (*ran)[len(*ran)-1] != "fprintd-verify" {
		t.Errorf("ran %v", *ran)
	}
	handle(request{Cmd: "bio.clear", Kid: kidA})
	if o := handle(request{Cmd: "bio.open", Kid: kidA}); o["error"] != "none" {
		t.Errorf("after clear %v", o)
	}
	if r := handle(request{Cmd: "bio.seal", Kid: "nope"}); r["error"] != "bad" {
		t.Errorf("bad kid %v", r)
	}
}

func TestBioFingerprintRefused(t *testing.T) {
	fakeFprint(t, "User oros has no fingers enrolled for Goodix.\n", "Verify result: verify-no-match (done)\n", errors.New("exit 1"))
	if st := handle(request{Cmd: "bio.state"}); st["can"] != "none" {
		t.Errorf("state %v", st)
	}
	if r := handle(request{Cmd: "bio.seal", Kid: kidA}); r["error"] != "failed" || r["d"] != nil {
		t.Errorf("no match %v", r)
	}
	if _, err := os.Stat(bioPathFn()); !os.IsNotExist(err) {
		t.Error("kept after a failed touch")
	}
	fprintLook = func(string) (string, error) { return "", errors.New("missing") }
	if st := handle(request{Cmd: "bio.state"}); st["can"] != "no-hw" || st["kind"] != "" {
		t.Errorf("no fprintd %v", st)
	}
}

func TestFprintParsing(t *testing.T) {
	if fprintCan("No devices available\n") != "no-hw" || fprintCan("") != "no-hw" {
		t.Error("no reader")
	}
	if fprintVerdict("verify-match", errors.New("exit 1"), false) == "" {
		t.Error("a failing exit is not a pass")
	}
	if fprintVerdict("", errors.New("killed"), true) != "timeout" {
		t.Error("timeout")
	}
	if fprintVerdict("failed to claim device: Device was already claimed", errors.New("x"), false) != "busy" {
		t.Error("busy")
	}
}
