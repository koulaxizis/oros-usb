package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The keyring's password is made once, kept 0600 and reused; a login
// keyring that orOS did not make is left alone.
func TestKeyringKey(t *testing.T) {
	home := t.TempDir()
	k, err := keyringKey(home)
	if err != nil || len(k) != 64 {
		t.Fatalf("%q %v", k, err)
	}
	fi, err := os.Stat(keyringKeyFile(home))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v %v", fi, err)
	}
	if k2, err := keyringKey(home); err != nil || k2 != k {
		t.Fatalf("second time %q %v", k2, err)
	}

	other := t.TempDir()
	_ = os.MkdirAll(keyringDir(other), 0o700)
	_ = os.WriteFile(filepath.Join(keyringDir(other), "login.keyring"), []byte("x"), 0o600)
	if _, err := keyringKey(other); err == nil {
		t.Fatal("a foreign login keyring got an orOS key")
	}
	if _, err := os.Stat(keyringKeyFile(other)); err == nil {
		t.Fatal("key file written next to a foreign keyring")
	}

	bad := t.TempDir()
	_ = os.MkdirAll(keyringDir(bad), 0o700)
	_ = os.WriteFile(keyringKeyFile(bad), []byte("short\n"), 0o600)
	if _, err := keyringKey(bad); err == nil {
		t.Fatal("a broken key file was used")
	}
}

// With gnome-keyring and a session bus (dbus-run-session go test), the
// real thing: "oros-system keyring" opens the default collection without a
// prompt, again after the daemon is gone (the next boot).
func TestKeyringReal(t *testing.T) {
	if _, err := exec.LookPath(keyringDaemon); err != nil || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" || os.Getenv("OROS_KEYRING_TEST") == "" {
		t.Skip("needs gnome-keyring-daemon, a session bus and OROS_KEYRING_TEST=1")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	for boot := 1; boot <= 2; boot++ {
		if keyringMain(nil) != 0 {
			t.Fatalf("boot %d: keyring failed", boot)
		}
		col, err := keyringOpen()
		if err != nil {
			t.Fatalf("boot %d: %s %v", boot, col, err)
		}
		t.Logf("boot %d: %s open", boot, col)
		_ = exec.Command("pkill", "-x", "gnome-keyring-d").Run()
		time.Sleep(time.Second)
	}
	if _, err := os.Stat(filepath.Join(keyringDir(home), "login.keyring")); err != nil {
		t.Fatal(err)
	}
}
