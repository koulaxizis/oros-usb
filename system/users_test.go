package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSignedInUser(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "oros", "data")
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(data, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if d, o := signedInUser(home); d != data || !o {
		t.Fatal("no users: the data folder, the owner")
	}
	write("users.json", `{"users":[{"id":"u00000001","owner":true,"dir":""},{"id":"u00000002","dir":"users/u00000002"},{"id":"u00000003","dir":"../../x"}],"guest":true}`)
	if d, o := signedInUser(home); d != data || o {
		t.Fatal("nobody signed in: not the owner")
	}
	write("users/current.json", `{"id":"u00000002"}`)
	if d, o := signedInUser(home); d != filepath.Join(data, "users", "u00000002") || o {
		t.Fatalf("Anna: %s %v", d, o)
	}
	if got := webArgs(home, webFallback)[1]; got != "--user-data-dir="+filepath.Join(data, "users", "u00000002", "profile") {
		t.Fatalf("web profile %s", got)
	}
	write("users/current.json", `{"id":"u00000001"}`)
	if d, o := signedInUser(home); d != data || !o {
		t.Fatal("owner")
	}
	write("users/current.json", `{"id":"u00000003"}`)
	if d, o := signedInUser(home); d != data || o {
		t.Fatal("a folder outside data must never be used")
	}
	write("users/current.json", `{"id":"guest"}`)
	if d, o := signedInUser(home); d != filepath.Join(data, "users", "guest") || o {
		t.Fatal("guest")
	}
}

// The Internet button opens orOS's Start page when the served orOS has
// it, DuckDuckGo when it does not (an older orOS) or nothing answers.
func TestWebHomeStartPage(t *testing.T) {
	old := webStart
	defer func() { webStart = old }()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/startpage/" {
			http.NotFound(w, r)
		}
	}))
	defer ok.Close()
	webStart = ok.URL + "/startpage/"
	if got := webHome(); got != webStart {
		t.Fatalf("with a Start page: %q", got)
	}
	webStart = ok.URL + "/missing/"
	if got := webHome(); got != webFallback {
		t.Fatalf("older orOS: %q", got)
	}
	ok.Close()
	webStart = ok.URL + "/startpage/"
	if got := webHome(); got != webFallback {
		t.Fatalf("nothing answers: %q", got)
	}
}
