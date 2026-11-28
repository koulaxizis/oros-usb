package main

// The session's keyring (the Secret Service on D-Bus, gnome-keyring).
// Programs from the software center keep their passwords there: Proton
// Mail Bridge keeps the key of its own encrypted store in it and does not
// sign in without one. orOS on the stick has no login password, so no
// login can unlock a keyring; a keyring without a password makes
// gnome-keyring ask for one in a window instead. So the login keyring gets
// a random password of its own, kept next to it in the home folder (0600),
// and "oros-system keyring" unlocks it with that at every session start,
// before any program asks: nobody is ever asked anything.
//
// What this protects: the same as the home folder. With encrypted data
// (ENCRYPTION.md) the keyring is encrypted with everything else; without
// it, whoever has the stick can read it, as on any computer that signs in
// by itself.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

var keyringDaemon = "gnome-keyring-daemon"

func keyringDir(home string) string     { return filepath.Join(home, ".local", "share", "keyrings") }
func keyringKeyFile(home string) string { return filepath.Join(keyringDir(home), ".oros-key") }

// keyringKey returns the login keyring's password, made the first time.
// A login keyring that is there without the password was not made by
// orOS: it is left alone (unlocking it would fail and ask in a window).
func keyringKey(home string) (string, error) {
	b, err := os.ReadFile(keyringKeyFile(home))
	if err == nil {
		k := strings.TrimSpace(string(b))
		if len(k) != 64 {
			return "", errors.New("keyring: bad key file")
		}
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(keyringDir(home), "login.keyring")); err == nil {
		return "", errors.New("keyring: a login keyring without the orOS key")
	}
	if err := os.MkdirAll(keyringDir(home), 0o700); err != nil {
		return "", err
	}
	r := make([]byte, 32)
	if _, err := rand.Read(r); err != nil {
		return "", err
	}
	k := hex.EncodeToString(r)
	tmp := keyringKeyFile(home) + ".tmp"
	if err := os.WriteFile(tmp, []byte(k+"\n"), 0o600); err != nil {
		return "", err
	}
	return k, os.Rename(tmp, keyringKeyFile(home))
}

// keyringMain is "oros-system keyring" (oros-session, once per boot): the
// Secret Service with the login keyring open. It replaces a daemon that
// D-Bus or systemd may have started locked.
func keyringMain(args []string) int {
	if _, err := exec.LookPath(keyringDaemon); err != nil {
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	k, err := keyringKey(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(keyringDaemon, "--replace", "--unlock", "--daemonize", "--components=secrets")
	cmd.Stdin = strings.NewReader(k)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keyring: %v %s\n", err, strings.TrimSpace(string(out)))
		return 1
	}
	return 0
}

// keyringOpen asks the Secret Service which collection is the default and
// whether it is unlocked: what Proton Mail Bridge needs.
func keyringOpen() (string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "", err
	}
	svc := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	var col dbus.ObjectPath
	if err := svc.Call("org.freedesktop.Secret.Service.ReadAlias", 0, "default").Store(&col); err != nil {
		return "", err
	}
	if col == "/" || col == "" {
		return "", errors.New("no default collection")
	}
	v, err := conn.Object("org.freedesktop.secrets", col).GetProperty("org.freedesktop.Secret.Collection.Locked")
	if err != nil {
		return "", err
	}
	if locked, _ := v.Value().(bool); locked {
		return string(col), errors.New(string(col) + " is locked")
	}
	return string(col), nil
}

// selftestKeyring (boot tests, inside labwc): the default collection is
// there and open without anyone typing a password.
func selftestKeyring(check func(string, error)) {
	var col string
	var err error
	for i := 0; i < 10; i++ {
		if col, err = keyringOpen(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	check("keyring (Secret Service) "+col+" unlocked", err)
}
