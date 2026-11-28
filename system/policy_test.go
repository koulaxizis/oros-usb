package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"testing"
)

// extensionID is Chromium's ID for an extension with this manifest key:
// the first 16 bytes of the SHA-256 of the key, one letter a-p per hex digit.
func extensionID(t *testing.T, key string) string {
	der, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	sum := sha256.Sum256(der)
	id := make([]byte, 32)
	for i, b := range sum[:16] {
		id[2*i] = 'a' + b>>4
		id[2*i+1] = 'a' + b&15
	}
	return string(id)
}

// The managed policy keeps every extension away from orOS's pages
// (localhost, useoros.online) except the System extension and the orOS
// add-on, so a VPN or other extension installed in the Internet window
// cannot read an unlocked orOS. Their IDs in the policy must be the ones
// their keys give.
func TestPolicyLetsOnlyOrOSExtensionsReadOrOS(t *testing.T) {
	b, err := os.ReadFile("../config/includes.chroot/etc/chromium/policies/managed/oros.json")
	if err != nil {
		t.Fatal(err)
	}
	var pol struct {
		ExtensionSettings map[string]struct {
			Blocked *[]string `json:"runtime_blocked_hosts"`
		}
	}
	if err := json.Unmarshal(b, &pol); err != nil {
		t.Fatal(err)
	}
	all, ok := pol.ExtensionSettings["*"]
	if !ok || all.Blocked == nil {
		t.Fatal("no runtime_blocked_hosts for every extension")
	}
	got := append([]string(nil), *all.Blocked...)
	sort.Strings(got)
	want := []string{"*://*.useoros.online", "*://127.0.0.1", "*://localhost"}
	if len(got) != len(want) {
		t.Fatalf("blocked hosts %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blocked hosts %v, want %v", got, want)
		}
	}

	var sys struct{ Key string }
	mb, err := os.ReadFile("extension/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mb, &sys); err != nil {
		t.Fatal(err)
	}
	sh, err := os.ReadFile("../scripts/addon.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^KEY="([A-Za-z0-9+/=]+)"$`).FindSubmatch(sh)
	if m == nil {
		t.Fatal("scripts/addon.sh sets no KEY")
	}
	ours := map[string]bool{extensionID(t, sys.Key): true, extensionID(t, string(m[1])): true}
	if len(pol.ExtensionSettings) != len(ours)+1 {
		t.Fatalf("ExtensionSettings has %d entries, want * and the 2 orOS extensions", len(pol.ExtensionSettings))
	}
	for id := range ours {
		e, ok := pol.ExtensionSettings[id]
		if !ok || e.Blocked == nil || len(*e.Blocked) != 0 {
			t.Fatalf("extension %s: want its own entry with an empty runtime_blocked_hosts", id)
		}
	}
}

// oros-session starts Chromium with --enable-speech-dispatcher (orOS's
// read-aloud with the computer's own voices). Chromium counts it among its
// "unsupported" flags and shows a yellow warning bar over orOS at every
// start unless the managed policy turns those warnings off (a 2026-11-28
// report from a real USB). Both halves must stay together.
func TestSpeechDispatcherFlagHasNoWarningBar(t *testing.T) {
	sess, err := os.ReadFile("../config/includes.chroot/usr/local/bin/oros-session")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^ARGS="[^"]*--enable-speech-dispatcher[ "]`).Match(sess) {
		t.Skip("oros-session no longer passes --enable-speech-dispatcher")
	}
	b, err := os.ReadFile("../config/includes.chroot/etc/chromium/policies/managed/oros.json")
	if err != nil {
		t.Fatal(err)
	}
	var pol map[string]any
	if err := json.Unmarshal(b, &pol); err != nil {
		t.Fatal(err)
	}
	if v, ok := pol["CommandLineFlagSecurityWarningsEnabled"].(bool); !ok || v {
		t.Fatalf("CommandLineFlagSecurityWarningsEnabled = %v, want false", pol["CommandLineFlagSecurityWarningsEnabled"])
	}
}
