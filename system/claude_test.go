package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

func TestDebCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0}, {"1.0", "1.1", -1}, {"1.10", "1.9", 1}, {"1.0~beta1", "1.0", -1},
		{"1.0", "1.0-1", -1}, {"1:0.9", "2.0", 1}, {"0.14.10", "0.14.9", 1}, {"1.0a", "1.0+", -1},
		{"1.2.3-2", "1.2.3-10", -1}, {"01.0", "1.0", 0},
	} {
		if got := debCompare(c.a, c.b); got != c.want {
			t.Errorf("debCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := debCompare(c.b, c.a); got != -c.want {
			t.Errorf("debCompare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestClaudePick(t *testing.T) {
	sum := strings.Repeat("a", 64)
	st := func(v, arch, fn string) string {
		return fmt.Sprintf("Package: claude-desktop\nVersion: %s\nArchitecture: %s\nFilename: %s\nSize: 10\nSHA256: %s\nDescription: x\n continued\n", v, arch, fn, sum)
	}
	idx := strings.Join([]string{
		st("1.2.0", "amd64", "pool/main/c/claude-desktop/claude-desktop_1.2.0_amd64.deb"),
		st("1.10.0", "amd64", "pool/main/c/claude-desktop/claude-desktop_1.10.0_amd64.deb"),
		st("9.0.0", "arm64", "pool/main/c/claude-desktop/claude-desktop_9.0.0_arm64.deb"),
		st("99.0.0", "amd64", "pool/../../etc/evil.deb"),
		st("98.0.0", "amd64", "https://evil.example/x.deb"),
		strings.Replace(st("97.0.0", "amd64", "pool/x.deb"), "claude-desktop\n", "other\n", 1),
	}, "\n")
	p, err := claudePick([]byte(idx), "amd64")
	if err != nil || p.version != "1.10.0" || p.size != 10 || p.sum != sum {
		t.Fatalf("pick amd64: %+v %v", p, err)
	}
	if p, err := claudePick([]byte(idx), "arm64"); err != nil || p.version != "9.0.0" {
		t.Fatalf("pick arm64: %+v %v", p, err)
	}
	if _, err := claudePick([]byte(idx), "riscv64"); err == nil || !strings.HasPrefix(err.Error(), "os:") {
		t.Fatalf("riscv64: %v", err)
	}
}

func TestClaudeResolveStaysInside(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "usr/bin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(root, "usr/lib/app"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "usr/lib/app/claude"), []byte("\x7fELFxx"), 0o755))
	must(t, os.Symlink("/usr/lib/app/claude", filepath.Join(root, "usr/bin/abs")))
	must(t, os.Symlink("../lib/app/claude", filepath.Join(root, "usr/bin/rel")))
	must(t, os.Symlink("../../../../../../bin/sh", filepath.Join(root, "usr/bin/out")))
	for _, p := range []string{"/usr/bin/abs", "/usr/bin/rel"} {
		if rel, ok := claudeResolve(root, p); !ok || rel != "usr/lib/app/claude" {
			t.Errorf("%s: %q %v", p, rel, ok)
		}
	}
	// "../../bin/sh" from inside root is /bin/sh of root, which is not there
	if rel, ok := claudeResolve(root, "/usr/bin/out"); ok {
		t.Errorf("left root: %q", rel)
	}
}

// claudeKey makes a signing key and writes its public half to a keyring
// file; the pinned fingerprint is set to it for the test.
func claudeKey(t *testing.T) (*openpgp.Entity, string) {
	t.Helper()
	e, err := openpgp.NewEntity("Test repo", "", "repo@example.invalid", nil)
	must(t, err)
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	must(t, err)
	must(t, e.Serialize(w))
	must(t, w.Close())
	f := filepath.Join(t.TempDir(), "key.asc")
	must(t, os.WriteFile(f, b.Bytes(), 0o644))
	old := claudeFingerprint
	claudeFingerprint = fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
	t.Cleanup(func() { claudeFingerprint = old })
	return e, f
}

func clearSign(t *testing.T, e *openpgp.Entity, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := clearsign.Encode(&b, e.PrivateKey, nil)
	must(t, err)
	_, err = w.Write([]byte(text))
	must(t, err)
	must(t, w.Close())
	return b.Bytes()
}

// fakeClaudeDeb builds a .deb shaped like an Electron app: the program in
// /usr/lib/claude-desktop next to resources/app.asar, linked from
// /usr/bin/claude-desktop. The program is a copy of /bin/true.
func fakeClaudeDeb(t *testing.T, version string) []byte {
	t.Helper()
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb missing")
	}
	src := t.TempDir()
	lib := filepath.Join(src, "usr/lib/claude-desktop")
	must(t, os.MkdirAll(filepath.Join(lib, "resources"), 0o755))
	must(t, os.MkdirAll(filepath.Join(src, "usr/bin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(src, "DEBIAN"), 0o755))
	tr, err := os.ReadFile("/bin/true")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(lib, "claude-desktop"), tr, 0o755))
	must(t, os.WriteFile(filepath.Join(lib, "resources/app.asar"), []byte("asar"), 0o644))
	must(t, os.Symlink("/usr/lib/claude-desktop/claude-desktop", filepath.Join(src, "usr/bin/claude-desktop")))
	must(t, os.WriteFile(filepath.Join(src, "DEBIAN/control"), []byte("Package: claude-desktop\nVersion: "+version+"\nArchitecture: amd64\nMaintainer: test <t@example.invalid>\nDescription: test\n"), 0o644))
	out := filepath.Join(t.TempDir(), "c.deb")
	if b, err := exec.Command("dpkg-deb", "--root-owner-group", "--build", src, out).CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v %s", err, b)
	}
	b, err := os.ReadFile(out)
	must(t, err)
	return b
}

type claudeRepoFake struct {
	inRelease, packagesGz, deb []byte
	served                     map[string]int
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func newClaudeRepo(t *testing.T, e *openpgp.Entity, deb []byte, version string) *claudeRepoFake {
	t.Helper()
	pkgs := fmt.Sprintf("Package: claude-desktop\nVersion: %s\nArchitecture: amd64\nFilename: pool/main/c/claude-desktop/claude-desktop_%s_amd64.deb\nSize: %d\nSHA256: %s\n", version, version, len(deb), sha(deb))
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(pkgs))
	must(t, zw.Close())
	rel := fmt.Sprintf("Origin: test\nSuite: stable\nArchitectures: amd64 arm64\nComponents: main\nSHA256:\n %s %d main/binary-amd64/Packages.gz\n", sha(gz.Bytes()), gz.Len())
	return &claudeRepoFake{inRelease: clearSign(t, e, rel), packagesGz: gz.Bytes(), deb: deb, served: map[string]int{}}
}

func (r *claudeRepoFake) serve(t *testing.T) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.served[q.URL.Path]++
		switch {
		case q.URL.Path == "/claude-desktop/apt/stable/dists/stable/InRelease":
			w.Write(r.inRelease)
		case q.URL.Path == "/claude-desktop/apt/stable/dists/stable/main/binary-amd64/Packages.gz":
			w.Write(r.packagesGz)
		case strings.HasPrefix(q.URL.Path, "/claude-desktop/apt/stable/pool/"):
			w.Write(r.deb)
		default:
			http.NotFound(w, q)
		}
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	old := claudeHTTP
	claudeHTTP = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(func() { claudeHTTP = old })
}

func claudeTestOpts(t *testing.T, keyring string) claudeOpts {
	return claudeOpts{dir: t.TempDir(), repo: claudeRepo, keyring: keyring, arch: "amd64"}
}

func TestClaudeFetchInstallsAndUpdates(t *testing.T) {
	e, keyring := claudeKey(t)
	repo := newClaudeRepo(t, e, fakeClaudeDeb(t, "1.0.0"), "1.0.0")
	repo.serve(t)
	o := claudeTestOpts(t, keyring)
	var steps []string
	if err := claudeFetch(o, func(s string, _ int) { steps = append(steps, s) }); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	a, ok := claudeInstalled(o.dir)
	if !ok || a.Version != "1.0.0" || a.Exec != "usr/lib/claude-desktop/claude-desktop" {
		t.Fatalf("installed: %+v %v", a, ok)
	}
	if !strings.Contains(strings.Join(steps, " "), "verify") {
		t.Errorf("steps %v", steps)
	}
	if _, err := os.Stat(filepath.Join(o.dir, "download.deb")); err == nil {
		t.Error("the .deb was kept")
	}
	// the same version again downloads no package
	n := repo.served["/claude-desktop/apt/stable/pool/main/c/claude-desktop/claude-desktop_1.0.0_amd64.deb"]
	must(t, claudeFetch(o, func(string, int) {}))
	if repo.served["/claude-desktop/apt/stable/pool/main/c/claude-desktop/claude-desktop_1.0.0_amd64.deb"] != n {
		t.Error("downloaded an up-to-date version again")
	}
	// a newer one replaces it
	*repo = *newClaudeRepo(t, e, fakeClaudeDeb(t, "1.1.0"), "1.1.0")
	must(t, claudeFetch(o, func(string, int) {}))
	if a, ok := claudeInstalled(o.dir); !ok || a.Version != "1.1.0" {
		t.Fatalf("update: %+v %v", a, ok)
	}
}

func TestClaudeFetchRefusesTampering(t *testing.T) {
	e, keyring := claudeKey(t)
	deb := fakeClaudeDeb(t, "1.0.0")

	t.Run("deb", func(t *testing.T) {
		repo := newClaudeRepo(t, e, deb, "1.0.0")
		bad := append([]byte{}, deb...)
		bad[len(bad)-1] ^= 1
		repo.deb = bad
		repo.serve(t)
		o := claudeTestOpts(t, keyring)
		if err := claudeFetch(o, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("tampered deb: %v", err)
		}
		if _, ok := claudeInstalled(o.dir); ok {
			t.Fatal("installed a tampered package")
		}
	})
	t.Run("index", func(t *testing.T) {
		repo := newClaudeRepo(t, e, deb, "1.0.0")
		repo.packagesGz = append(repo.packagesGz, 0)
		repo.serve(t)
		if err := claudeFetch(claudeTestOpts(t, keyring), func(string, int) {}); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("tampered index: %v", err)
		}
	})
	t.Run("signature", func(t *testing.T) {
		other, err := openpgp.NewEntity("Someone else", "", "x@example.invalid", nil)
		must(t, err)
		repo := newClaudeRepo(t, other, deb, "1.0.0")
		repo.serve(t)
		// the image's key does not check it, and the published key (here
		// not served) cannot be fetched: nothing is installed
		o := claudeTestOpts(t, keyring)
		if err := claudeFetch(o, func(string, int) {}); err == nil {
			t.Fatal("accepted another key's signature")
		}
		if _, ok := claudeInstalled(o.dir); ok {
			t.Fatal("installed")
		}
	})
	t.Run("unsigned", func(t *testing.T) {
		repo := newClaudeRepo(t, e, deb, "1.0.0")
		repo.inRelease = []byte("Origin: test\nSHA256:\n")
		repo.serve(t)
		if err := claudeFetch(claudeTestOpts(t, keyring), func(string, int) {}); err == nil || !strings.Contains(err.Error(), "not signed") {
			t.Fatalf("unsigned: %v", err)
		}
	})
	t.Run("pinned fingerprint", func(t *testing.T) {
		repo := newClaudeRepo(t, e, deb, "1.0.0")
		repo.serve(t)
		old := claudeFingerprint
		claudeFingerprint = strings.Repeat("0", 40)
		defer func() { claudeFingerprint = old }()
		if err := claudeFetch(claudeTestOpts(t, keyring), func(string, int) {}); err == nil {
			t.Fatal("accepted a key with another fingerprint")
		}
	})
}

func TestClaudeRemoveKeepsNothingButSettings(t *testing.T) {
	e, keyring := claudeKey(t)
	repo := newClaudeRepo(t, e, fakeClaudeDeb(t, "1.0.0"), "1.0.0")
	repo.serve(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	o := claudeTestOpts(t, keyring)
	o.dir = claudeDir(home)
	must(t, os.MkdirAll(filepath.Join(home, "oros", "data"), 0o755))
	must(t, claudeFetch(o, func(string, int) {}))
	st := claudeStatus()
	if st["installed"] != true || st["version"] != "1.0.0" {
		t.Fatalf("status %v", st)
	}
	cfg := filepath.Join(home, ".config", "Claude")
	must(t, os.MkdirAll(cfg, 0o700))
	if r := claudeRemove(); r["ok"] != true {
		t.Fatalf("remove %v", r)
	}
	if st := claudeStatus(); st["installed"] != false {
		t.Fatalf("after remove %v", st)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Error("removed the person's Claude settings")
	}
}
