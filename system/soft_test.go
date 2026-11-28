package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoCatalog(t *testing.T) catalog {
	t.Helper()
	b, err := os.ReadFile("../catalog/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseCatalog(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The catalog in the repository is valid, every entry has its three
// languages, orOS's own come first and the game stores are there.
func TestRepoCatalog(t *testing.T) {
	c := repoCatalog(t)
	if c.Items[0].Kind != "builtin" {
		t.Errorf("orOS's own apps come first, got %s", c.Items[0].ID)
	}
	for _, it := range c.Items {
		for _, lang := range []string{"en", "el", "de"} {
			if it.Name[lang] == "" || it.Summary[lang] == "" {
				t.Errorf("%s: no %s text", it.ID, lang)
			}
		}
	}
	for _, m := range c.Models {
		if m.Licence == "" || m.By == "" || m.MinMemGiB <= 0 {
			t.Errorf("model %s: licence, maker and memory needed", m.Name)
		}
		for _, lang := range []string{"en", "el", "de"} {
			if m.Summary[lang] == "" {
				t.Errorf("model %s: no %s text", m.Name, lang)
			}
		}
		for _, q := range aiModels {
			if q.Name == m.Name {
				t.Errorf("model %s is already offered", m.Name)
			}
		}
	}
	for _, id := range []string{"com.valvesoftware.Steam", "com.heroicgameslauncher.hgl", "io.itch.itch"} {
		if _, ok := c.item(id, "flatpak"); !ok {
			t.Errorf("%s missing", id)
		}
	}
}

func TestParseCatalogRefuses(t *testing.T) {
	bad := []string{
		`{"version":2,"items":[]}`,
		`{"version":1,"items":[{"id":"com.x.Y; rm -rf","kind":"flatpak","mb":1,"name":{"en":"x"}}]}`,
		`{"version":1,"items":[{"id":"com.x.Y","kind":"flatpak","name":{"en":"x"}}]}`,
		`{"version":1,"items":[{"id":"ABCDEFGHIJKLMNOPABCDEFGHIJKLMNOP","kind":"addon","name":{"en":"x"}}]}`,
		`{"version":1,"items":[{"id":"steam","kind":"builtin","name":{"en":"x"}}]}`,
		`{"version":1,"items":[{"id":"ai","kind":"builtin","name":{"en":"x"}},{"id":"ai","kind":"builtin","name":{"en":"x"}}]}`,
		`{"version":1,"items":[{"id":"ai","kind":"builtin"}]}`,
		`{"version":1,"items":[{"id":"x","kind":"deb","name":{"en":"x"}}]}`,
		`{"version":1,"items":[],"models":[{"name":"../x","mb":1}]}`,
		`{"version":1,"items":[],"models":[{"name":"x:1b","mb":0}]}`,
	}
	for _, b := range bad {
		if _, err := parseCatalog([]byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestFlathubFilter(t *testing.T) {
	c := repoCatalog(t)
	f := flathubFilter(c)
	lines := strings.Split(strings.TrimSpace(f), "\n")
	if lines[0] != "deny app/*" {
		t.Fatalf("filter starts %q", lines[0])
	}
	n := 0
	for _, it := range c.Items {
		if it.Kind == "flatpak" {
			n++
			if !strings.Contains(f, "\nallow app/"+it.ID+"\n") {
				t.Errorf("%s not allowed", it.ID)
			}
		}
	}
	if len(lines) != n+1 {
		t.Errorf("%d lines for %d apps", len(lines), n)
	}
	if strings.Contains(f, "runtime/") {
		t.Error("runtimes must stay allowed")
	}
}

func TestParseFlatpakList(t *testing.T) {
	m := parseFlatpakList("com.valvesoftware.Steam\t1.0.0.82\nio.itch.itch\t\n\nApplication ID\tVersion\n")
	if m["com.valvesoftware.Steam"] != "1.0.0.82" || len(m) != 2 {
		t.Fatalf("%v", m)
	}
	if _, ok := m["io.itch.itch"]; !ok {
		t.Fatal("itch missing")
	}
}

func TestFlatpakPercent(t *testing.T) {
	for _, c := range []struct {
		line string
		step string
		pct  int
		ok   bool
	}{
		{"Installing 1/4… ████▌ 37%  2.1 MB/s  00:12", "installing", 10, true},
		{"Installing 4/4… 100%", "installing", 99, true},
		{"Updating 2/3…", "updating", 34, true},
		{"Uninstalling 1/1…", "uninstalling", 2, true},
		{"Looking for matches…", "", 0, false},
		{"Installing 5/4…", "", 0, false},
	} {
		step, pct, ok := flatpakPercent(c.line)
		if ok != c.ok || step != c.step || pct != c.pct {
			t.Errorf("%q: %q %d %v", c.line, step, pct, ok)
		}
	}
}

func TestScanLinesCR(t *testing.T) {
	adv, tok, _ := scanLinesCR([]byte("a 1%\rb 2%\n"), false)
	if adv != 5 || string(tok) != "a 1%" {
		t.Fatalf("%d %q", adv, tok)
	}
}

func TestAddonPolicy(t *testing.T) {
	c := repoCatalog(t)
	var addon string
	for _, it := range c.Items {
		if it.Kind == "addon" {
			addon = it.ID
			break
		}
	}
	if addon == "" {
		t.Fatal("no add-on in the catalog")
	}
	dir := t.TempDir()
	list := filepath.Join(dir, "data", "addons.json")
	// a valid id that is not in the catalog never reaches the policy
	other := strings.Repeat("a", 32)
	if err := writeAddonList(list, map[string]bool{addon: true, other: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(list+".x", []byte(`{"on":["../../etc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(readAddonList(list+".x")) != 0 {
		t.Error("a bad id was read")
	}
	b := addonPolicy(c, readAddonList(list))
	var p map[string][]string
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	want := addon + ";https://clients2.google.com/service/update2/crx"
	if len(p["ExtensionInstallForcelist"]) != 1 || p["ExtensionInstallForcelist"][0] != want {
		t.Fatalf("%v", p)
	}
	pol := filepath.Join(dir, "policies", "managed", "oros-addons.json")
	if err := writeAddonPolicy(pol, b); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(pol); string(got) != string(b) {
		t.Fatal("policy not written")
	}
	if l, _ := os.ReadDir(filepath.Dir(pol)); len(l) != 1 {
		t.Fatalf("%d files in the policy folder", len(l))
	}
	if addonPolicy(c, map[string]bool{other: true}) != nil {
		t.Fatal("policy for no catalog add-on")
	}
	if err := writeAddonPolicy(pol, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pol); !os.IsNotExist(err) {
		t.Fatal("empty policy kept")
	}
	if err := writeAddonPolicy(pol, nil); err != nil {
		t.Fatal("removing twice", err)
	}
}

// The models the catalog adds are offered and can be installed by name.
func TestCatalogModelsOffered(t *testing.T) {
	old := catalogFile
	defer func() { catalogFile = old }()
	catalogFile = "../catalog/catalog.json"
	c := repoCatalog(t)
	for _, m := range c.Models {
		if mb, ok := aiModelMB(m.Name); !ok || mb != m.MB {
			t.Errorf("%s: %d %v", m.Name, mb, ok)
		}
	}
	if _, ok := aiModelMB("llama3:8b"); ok {
		t.Error("a model outside the catalog")
	}
}

// Proton Mail Bridge is in the catalog as a program (not a game store),
// and every program that starts with the session is a catalog app.
func TestBackgroundApps(t *testing.T) {
	c := repoCatalog(t)
	it, ok := c.item("ch.protonmail.protonmail-bridge", "flatpak")
	if !ok || it.Section != "apps" {
		t.Fatalf("Proton Mail Bridge: %v %q", ok, it.Section)
	}
	for _, id := range backgroundIDs() {
		if _, ok := c.item(id, "flatpak"); !ok {
			t.Errorf("%s starts with the session but is not a catalog app", id)
		}
		for _, a := range backgroundApps[id] {
			if !strings.HasPrefix(a, "--") {
				t.Errorf("%s: argument %q", id, a)
			}
		}
	}
	if !strings.Contains(flathubFilter(c), "allow app/ch.protonmail.protonmail-bridge\n") {
		t.Error("Bridge not allowed by the Flathub filter")
	}
}
