package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTorLinks(t *testing.T) {
	good := `{"version":"14.5.9","downloads":{"linux-x86_64":{"ALL":{"binary":"https://dist.torproject.org/torbrowser/14.5.9/tor-browser-linux-x86_64-14.5.9.tar.xz","sig":"https://dist.torproject.org/torbrowser/14.5.9/tor-browser-linux-x86_64-14.5.9.tar.xz.asc"}}}}`
	v, bin, sig, err := torLinks([]byte(good))
	if err != nil || v != "14.5.9" || !strings.HasSuffix(bin, ".tar.xz") || !strings.HasSuffix(sig, ".asc") {
		t.Fatalf("good: %q %q %q %v", v, bin, sig, err)
	}
	old := strings.Replace(good, "linux-x86_64", "linux64", 1)
	if _, _, _, err := torLinks([]byte(old)); err != nil {
		t.Fatalf("linux64: %v", err)
	}
	for _, bad := range []string{
		strings.Replace(good, "https://dist.torproject.org", "https://torproject.org.evil.example", 1),
		strings.Replace(good, "https://dist.torproject.org", "http://dist.torproject.org", 1),
		`{"version":"1","downloads":{"windows-x86_64":{"ALL":{"binary":"https://dist.torproject.org/a","sig":"https://dist.torproject.org/a.asc"}}}}`,
		`not json`,
	} {
		if _, _, _, err := torLinks([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// torServer serves a fake Tor Browser on every *.torproject.org address.
func torServer(t *testing.T, tarball []byte) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/downloads.json":
			fmt.Fprint(w, `{"version":"99.0","downloads":{"linux-x86_64":{"ALL":{"binary":"https://dist.torproject.org/tb.tar.xz","sig":"https://dist.torproject.org/tb.tar.xz.asc"}}}}`)
		case r.URL.Path == "/tb.tar.xz":
			w.Write(tarball)
		case r.URL.Path == "/tb.tar.xz.asc":
			fmt.Fprint(w, "signature")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	old := torHTTP
	torHTTP = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(func() { torHTTP = old })
}

func fakeTorTarball(t *testing.T, marker string) []byte {
	t.Helper()
	src := t.TempDir()
	b := filepath.Join(src, "tor-browser", "Browser")
	must(t, os.MkdirAll(b, 0o755))
	must(t, os.WriteFile(filepath.Join(b, "start-tor-browser"), []byte("#!/bin/sh\necho \"$@\" > "+marker+"\n"), 0o755))
	out := filepath.Join(t.TempDir(), "tb.tar.xz")
	if b, err := exec.Command("tar", "-cJf", out, "-C", src, "tor-browser").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, b)
	}
	data, err := os.ReadFile(out)
	must(t, err)
	return data
}

func TestTorFetch(t *testing.T) {
	work := t.TempDir()
	marker := filepath.Join(work, "started")
	torServer(t, fakeTorTarball(t, marker))
	keyring := filepath.Join(work, "key.gpg")
	must(t, os.WriteFile(keyring, []byte("key"), 0o644))
	dir := filepath.Join(work, "tor")
	o := torOpts{dir: dir, index: "https://aus1.torproject.org/downloads.json", keyring: keyring, start: true}

	// A bad signature installs nothing.
	oldV := torVerify
	defer func() { torVerify = oldV }()
	torVerify = func(k, sig, file string) error { return errors.New("bad signature") }
	if err := torFetch(o, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("bad signature: %v", err)
	}
	if _, err := os.Stat(torStartScript(dir)); err == nil {
		t.Fatal("installed despite a bad signature")
	}

	var checked []string
	torVerify = func(k, sig, file string) error { checked = []string{k, sig, file}; return nil }
	var steps []string
	if err := torFetch(o, func(s string, _ int) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if checked[0] != keyring || checked[2] != filepath.Join(dir, "download.tar.xz") {
		t.Fatalf("verify args %q", checked)
	}
	if _, err := os.Stat(torStartScript(dir)); err != nil {
		t.Fatal(err)
	}
	for _, left := range []string{"download.tar.xz", "download.tar.xz.asc", "new", "tor-browser.old"} {
		if _, err := os.Stat(filepath.Join(dir, left)); err == nil {
			t.Errorf("%s left behind", left)
		}
	}
	got := strings.Join(steps, " ")
	for _, s := range []string{"index", "download", "verify", "unpack", "start"} {
		if !strings.Contains(got, s) {
			t.Errorf("no %s step in %q", s, got)
		}
	}
	var b []byte
	for i := 0; i < 50 && len(b) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ = os.ReadFile(marker)
	}
	if strings.TrimSpace(string(b)) != "--detach" {
		t.Fatalf("start args %q", b)
	}

	// A second fetch replaces the first one (the old copy goes away).
	o.start = false
	must(t, torFetch(o, func(string, int) {}))
	if _, err := os.Stat(torStartScript(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestTorBusyAndOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := torDir(home)
	must(t, os.MkdirAll(dir, 0o700))
	if torBusy(dir) {
		t.Fatal("busy without a download")
	}
	if r := handle(request{Cmd: "tor.status"}); r["ok"] != true || r["installed"] != false {
		t.Fatalf("status before: %v", r)
	}
	marker := filepath.Join(home, "started")
	must(t, os.MkdirAll(filepath.Dir(torStartScript(dir)), 0o755))
	must(t, os.WriteFile(torStartScript(dir), []byte("#!/bin/sh\necho \"$@\" > "+marker+"\n"), 0o755))
	if r := handle(request{Cmd: "tor.open"}); r["ok"] != true || r["started"] != true {
		t.Fatalf("open: %v", r)
	}
	var b []byte
	for i := 0; i < 50 && len(b) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		b, _ = os.ReadFile(marker)
	}
	if strings.TrimSpace(string(b)) != "--detach" {
		t.Fatalf("start args %q", b)
	}
	if r := handle(request{Cmd: "tor.status"}); r["ok"] != true || r["running"] != false || r["installed"] != true {
		t.Fatalf("status: %v", r)
	}
}
