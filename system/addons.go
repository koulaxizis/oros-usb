package main

// Add-ons from the software center (Chris 2026-11-22: VPN add-ons and more,
// «εύκολη εγκατάσταση χωρίς ιδιαίτερη ταλαιπωρία»). The owner picks them
// in Settings › Software; the list is kept in ~/oros/data/addons.json, and
// root ("oros-system addons --apply", one sudo rule, no arguments) writes
// Chromium's ExtensionInstallForcelist policy with the ones the catalog
// lists, at every session start (/etc is new at every boot) and after each
// change. Chromium reads the policy folder again by itself, installs the
// add-on from the Chrome Web Store, checks its signature and keeps it up to
// date; taking one off the list removes it. oros.json's "*" rule still
// keeps every add-on away from orOS's own pages.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

var (
	addonPolicyFile = "/etc/chromium/policies/managed/oros-addons.json"
	addonUpdateURL  = "https://clients2.google.com/service/update2/crx"
)

func addonListFile(home string) string { return filepath.Join(home, "oros", "data", "addons.json") }

// readAddonList: the ids the person chose (ids only; the catalog decides).
func readAddonList(file string) map[string]bool {
	m := map[string]bool{}
	b, err := os.ReadFile(file)
	if err != nil {
		return m
	}
	var l struct {
		On []string `json:"on"`
	}
	if json.Unmarshal(b, &l) != nil {
		return m
	}
	for _, id := range l.On {
		if addonIDRe.MatchString(id) {
			m[id] = true
		}
	}
	return m
}

func writeAddonList(file string, m map[string]bool) error {
	b, _ := json.Marshal(struct {
		On []string `json:"on"`
	}{sortedKeys(m)})
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(file+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

// addonPolicy is the policy file for these ids: only catalog add-ons, from
// the Chrome Web Store. nil means no file.
func addonPolicy(c catalog, on map[string]bool) []byte {
	var list []string
	for _, id := range sortedKeys(on) {
		if _, ok := c.item(id, "addon"); ok {
			list = append(list, id+";"+addonUpdateURL)
		}
	}
	if len(list) == 0 {
		return nil
	}
	b, _ := json.MarshalIndent(map[string][]string{"ExtensionInstallForcelist": list}, "", "  ")
	return append(b, '\n')
}

// addonSet is "soft.addon" (owner only: the policy is the computer's).
func addonSet(id string, on bool) reply {
	c, err := loadCatalog()
	if err != nil {
		return fail(err, "")
	}
	if _, ok := c.item(id, "addon"); !ok {
		return reply{"ok": false, "error": "not in the catalog"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	m := readAddonList(addonListFile(home))
	if on {
		m[id] = true
	} else {
		delete(m, id)
	}
	if err := writeAddonList(addonListFile(home), m); err != nil {
		return fail(err, "")
	}
	if out, err := run("sudo", "-n", "/opt/oros/oros-system", "addons", "--apply"); err != nil {
		return fail(err, out)
	}
	return reply{"ok": true, "installed": on}
}

// addonsMain is "oros-system addons --apply" (root, through sudo): the
// list of the user who ran sudo (the session's user without sudo).
func addonsMain(args []string) int {
	fl := flag.NewFlagSet("addons", flag.ContinueOnError)
	var apply bool
	fl.BoolVar(&apply, "apply", false, "write Chromium's policy from the chosen add-ons")
	if err := fl.Parse(args); err != nil || !apply {
		fmt.Fprintln(os.Stderr, "usage: oros-system addons --apply")
		return 2
	}
	if err := addonsApply(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func addonsApply() error {
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		name = "oros"
	}
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	c, err := loadCatalog()
	if err != nil {
		return err
	}
	return writeAddonPolicy(addonPolicyFile, addonPolicy(c, readAddonList(addonListFile(u.HomeDir))))
}

func writeAddonPolicy(file string, b []byte) error {
	if b == nil {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(file); err == nil && string(old) == string(b) {
		return nil
	}
	// Chromium reads every file in the policy folder: the half-written one
	// waits next to it.
	tmp := filepath.Join(filepath.Dir(filepath.Dir(file)), ".oros-addons.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
