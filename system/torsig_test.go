package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func sigKey(t *testing.T, pin bool) *openpgp.Entity {
	t.Helper()
	e, err := openpgp.NewEntity("Tor Browser Developers", "test", "test@example.org", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	must(t, err)
	if pin {
		old := torFingerprint
		torFingerprint = fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
		t.Cleanup(func() { torFingerprint = old })
	}
	return e
}

func pub(t *testing.T, e *openpgp.Entity) []byte {
	var b bytes.Buffer
	must(t, e.Serialize(&b))
	return b.Bytes()
}

func detachSign(t *testing.T, e *openpgp.Entity, data []byte, c *packet.Config) []byte {
	var b bytes.Buffer
	must(t, openpgp.ArmoredDetachSign(&b, e, bytes.NewReader(data), c))
	return b.Bytes()
}

// keyServer answers every *.torproject.org and keys.openpgp.org address.
func keyServer(t *testing.T) func(path string, b []byte) {
	t.Helper()
	var mu sync.Mutex
	files := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b, ok := files[r.URL.Path]
		mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
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
	return func(path string, b []byte) { mu.Lock(); files[path] = b; mu.Unlock() }
}

func TestTorSignature(t *testing.T) {
	set := keyServer(t)
	key := sigKey(t, true)
	other := sigKey(t, false)
	dir := t.TempDir()
	data := []byte("tor browser")
	files := map[string][]byte{
		"key.gpg": pub(t, key), "wrong.gpg": pub(t, other),
		"file": data, "file.asc": detachSign(t, key, data, nil),
		"bad": []byte("tor browser, changed"), "bad.asc": detachSign(t, key, data, nil),
		"other": data, "other.asc": detachSign(t, other, data, nil),
	}
	for n, b := range files {
		must(t, os.WriteFile(filepath.Join(dir, n), b, 0o644))
	}
	p := func(n string) string { return filepath.Join(dir, n) }
	if err := torVerifyGo(p("key.gpg"), p("file.asc"), p("file")); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	for _, c := range [][3]string{{"key.gpg", "bad.asc", "bad"}, {"key.gpg", "other.asc", "other"}, {"wrong.gpg", "other.asc", "other"}, {"none.gpg", "file.asc", "file"}} {
		if err := torVerifyGo(p(c[0]), p(c[1]), p(c[2])); err == nil {
			t.Errorf("%v passed", c)
		}
	}
	set("/vks/v1/by-fingerprint/"+torFingerprint, pub(t, other))
	if err := torVerifyGo(p("none.gpg"), p("other.asc"), p("other")); err == nil {
		t.Fatal("a fetched key with another fingerprint was used")
	}
	set("/vks/v1/by-fingerprint/"+torFingerprint, pub(t, key))
	if err := torVerifyGo(p("none.gpg"), p("file.asc"), p("file")); err != nil {
		t.Fatalf("fetched key: %v", err)
	}
}

// An older image's key cannot check a release signed by a newer subkey;
// the same key fetched again can.
func TestTorSignatureRefreshesKey(t *testing.T) {
	set := keyServer(t)
	key := sigKey(t, true)
	dir := t.TempDir()
	stale := pub(t, key)
	must(t, key.AddSigningSubkey(&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}))
	sub := key.Subkeys[len(key.Subkeys)-1]
	data := []byte("tor browser 2")
	must(t, os.WriteFile(filepath.Join(dir, "key.gpg"), stale, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "file"), data, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "file.asc"), detachSign(t, key, data, &packet.Config{SigningKeyId: sub.PublicKey.KeyId}), 0o644))
	p := func(n string) string { return filepath.Join(dir, n) }
	if err := torVerifyGo(p("key.gpg"), p("file.asc"), p("file")); err == nil {
		t.Fatal("the stale key checked a signature by a subkey it does not know")
	}
	set("/.well-known/openpgpkey/torproject.org/hu/kounek7zrdx745qydx6p59t9mqjpuhdf", pub(t, key))
	if err := torVerifyGo(p("key.gpg"), p("file.asc"), p("file")); err != nil {
		t.Fatalf("refreshed key: %v", err)
	}
}

func TestTorRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := torDir(home)
	must(t, os.MkdirAll(filepath.Dir(torStartScript(dir)), 0o755))
	must(t, os.WriteFile(torStartScript(dir), []byte("#!/bin/sh\n"), 0o755))
	if r := torStatus(); r["installed"] != true {
		t.Fatalf("status: %v", r)
	}
	if r := removeTor(); r["ok"] != true {
		t.Fatal(r)
	}
	if r := torStatus(); r["installed"] != false {
		t.Fatalf("still installed: %v", r)
	}
}
