package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func protonRelJSON(tag, host string) string {
	return fmt.Sprintf(`{"tag_name":%q,"draft":false,"prerelease":false,"assets":[
{"name":"%s.tar.gz","browser_download_url":"https://%s/GloriousEggroll/proton-ge-custom/releases/download/%s/%s.tar.gz","size":%d},
{"name":"%s.sha512sum","browser_download_url":"https://%s/GloriousEggroll/proton-ge-custom/releases/download/%s/%s.sha512sum","size":150}]}`,
		tag, tag, host, tag, tag, 1000, tag, host, tag, tag)
}

func TestProtonPick(t *testing.T) {
	tag, gz, sum, size, err := protonPick([]byte(protonRelJSON("GE-Proton10-25", "github.com")))
	if err != nil || tag != "GE-Proton10-25" || !strings.HasSuffix(gz, ".tar.gz") || !strings.HasSuffix(sum, ".sha512sum") || size != 1000 {
		t.Fatalf("pick: %q %q %q %d %v", tag, gz, sum, size, err)
	}
	for name, j := range map[string]string{
		"other host":  protonRelJSON("GE-Proton10-25", "example.com"),
		"bad tag":     protonRelJSON("GE-Proton10-25;rm", "github.com"),
		"path tag":    protonRelJSON("../x", "github.com"),
		"prerelease":  strings.Replace(protonRelJSON("GE-Proton10-25", "github.com"), `"prerelease":false`, `"prerelease":true`, 1),
		"no checksum": strings.Replace(protonRelJSON("GE-Proton10-25", "github.com"), `"name":"GE-Proton10-25.sha512sum"`, `"name":"x"`, 1),
		"not json":    "<html>",
	} {
		if _, _, _, _, err := protonPick([]byte(j)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestProtonURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://github.com/a/b":                         true,
		"https://objects.githubusercontent.com/x":        true,
		"https://release-assets.githubusercontent.com/x": true,
		"http://github.com/a":                            false,
		"https://github.com.evil.example/a":              false,
		"https://evilgithubusercontent.com/a":            false,
	} {
		if protonURL(u) != ok {
			t.Errorf("%s: want %v", u, ok)
		}
	}
}

func TestProtonSum(t *testing.T) {
	h := strings.Repeat("ab", 64)
	got, err := protonSum([]byte(h+"  GE-Proton10-25.tar.gz\n"), "GE-Proton10-25.tar.gz")
	if err != nil || got != h {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := protonSum([]byte(h+"  other.tar.gz\n"), "GE-Proton10-25.tar.gz"); err == nil {
		t.Error("checksum of another file accepted")
	}
	if _, err := protonSum([]byte("zz  GE-Proton10-25.tar.gz\n"), "GE-Proton10-25.tar.gz"); err == nil {
		t.Error("short checksum accepted")
	}
}

// The tool keeps one fixed internal name, so games set to it follow updates.
func TestProtonVDF(t *testing.T) {
	v := protonVDF("GE-Proton10-25")
	if !strings.Contains(v, `"`+protonName+`"`) || !strings.Contains(v, `"display_name" "GE-Proton (orOS) 10-25"`) {
		t.Fatal(v)
	}
}

func protonTarball(t *testing.T, tag string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: tag + "/" + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// protonServer answers as GitHub would, for any host name.
func protonServer(t *testing.T, tag string, gz []byte, sum string) (latest string, hits *int) {
	t.Helper()
	n := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "api.github.com":
			n++
			j := strings.Replace(protonRelJSON(tag, "github.com"), `"size":1000`, fmt.Sprintf(`"size":%d`, len(gz)), 1)
			fmt.Fprint(w, j)
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			// GitHub sends downloads on to its download host
			if r.Host == "github.com" {
				http.Redirect(w, r, "https://release-assets.githubusercontent.com"+r.URL.Path, http.StatusFound)
				return
			}
			_, _ = w.Write(gz)
		case strings.HasSuffix(r.URL.Path, ".sha512sum"):
			fmt.Fprint(w, sum)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	addr := srv.Listener.Addr().String()
	tr.TLSClientConfig.ServerName = "example.com" // the test certificate's name
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	old := protonHTTP
	protonHTTP = &http.Client{Transport: tr, CheckRedirect: protonRedirect}
	t.Cleanup(func() { protonHTTP = old })
	return "https://api.github.com/repos/GloriousEggroll/proton-ge-custom/releases/latest", &n
}

func TestProtonUpdate(t *testing.T) {
	home := t.TempDir()
	tag := "GE-Proton10-25"
	gz := protonTarball(t, tag, map[string]string{"proton": "#!/bin/sh\n", "compatibilitytool.vdf": "upstream", "files/bin/wine": "x", "version": "1 " + tag})
	s := sha512.Sum512(gz)
	latest, _ := protonServer(t, tag, gz, hex.EncodeToString(s[:])+"  "+tag+".tar.gz\n")

	// a version from ProtonPlus stays as it is
	pp := filepath.Join(protonToolsDir(home), "GE-Proton9-1")
	_ = os.MkdirAll(pp, 0o755)
	_ = os.WriteFile(filepath.Join(pp, "proton"), []byte("pp"), 0o755)

	var steps []string
	v, err := protonUpdate(home, latest, func(s string, _ int) { steps = append(steps, s) })
	if err != nil || v != tag {
		t.Fatalf("update: %q %v (steps %v)", v, err, steps)
	}
	dir := filepath.Join(protonToolsDir(home), protonFolder)
	if b, err := os.ReadFile(filepath.Join(dir, "compatibilitytool.vdf")); err != nil || string(b) != protonVDF(tag) {
		t.Fatalf("vdf: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "bin", "wine")); err != nil {
		t.Fatal(err)
	}
	if protonInstalled(home) != tag {
		t.Fatalf("installed %q", protonInstalled(home))
	}
	if _, err := os.Stat(filepath.Join(protonToolsDir(home), ".oros-new")); !os.IsNotExist(err) {
		t.Error("work folder left behind")
	}
	if b, _ := os.ReadFile(filepath.Join(pp, "proton")); string(b) != "pp" {
		t.Error("ProtonPlus's version was touched")
	}

	// the same version again: nothing is downloaded, the check time moves
	_ = os.WriteFile(filepath.Join(dir, "marker"), []byte("1"), 0o644)
	if v, err := protonUpdate(home, latest, func(string, int) {}); err != nil || v != tag {
		t.Fatalf("again: %q %v", v, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
		t.Error("an up-to-date GE-Proton was replaced")
	}
	if readProton(home).Checked == "" {
		t.Error("check time not kept")
	}
}

func TestProtonUpdateBadSum(t *testing.T) {
	home := t.TempDir()
	tag := "GE-Proton10-26"
	gz := protonTarball(t, tag, map[string]string{"proton": "x", "compatibilitytool.vdf": "x", "files/a": "x"})
	latest, _ := protonServer(t, tag, gz, strings.Repeat("0", 128)+"  "+tag+".tar.gz\n")
	if _, err := protonUpdate(home, latest, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(protonToolsDir(home), protonFolder)); !os.IsNotExist(err) {
		t.Error("an unchecked GE-Proton was installed")
	}
}

func TestProtonUpdateBadLayout(t *testing.T) {
	home := t.TempDir()
	tag := "GE-Proton10-27"
	gz := protonTarball(t, tag, map[string]string{"readme": "x"})
	s := sha512.Sum512(gz)
	latest, _ := protonServer(t, tag, gz, hex.EncodeToString(s[:])+"  "+tag+".tar.gz\n")
	if _, err := protonUpdate(home, latest, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("bad layout: %v", err)
	}
}

// Automatic updates are off until the owner turns them on.
func TestProtonAutoDefaultOff(t *testing.T) {
	home := t.TempDir()
	if readProton(home).Auto {
		t.Fatal("automatic GE-Proton is on by default")
	}
	if protonAutoDue(home) {
		t.Fatal("due without the switch")
	}
}
