package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// fakeOllama is an archive laid out like ollama-linux-amd64.tar.zst.
func fakeOllama(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			io.WriteString(tw, body)
		}
	}
	add(&tar.Header{Name: "bin/ollama", Mode: 0o755}, "#!/bin/sh\necho ollama\n")
	add(&tar.Header{Name: "lib/ollama/libggml-cpu-sandybridge.so", Mode: 0o755}, "cpu")
	add(&tar.Header{Name: "lib/ollama/libggml-base.so.0.28.0", Mode: 0o755}, "base")
	add(&tar.Header{Name: "lib/ollama/libggml-base.so.0", Typeflag: tar.TypeSymlink, Linkname: "libggml-base.so.0.28.0"}, "")
	add(&tar.Header{Name: "lib/ollama/LLAMA_CPP_LICENSE", Mode: 0o644}, "MIT")
	add(&tar.Header{Name: "lib/ollama/cuda_v12/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "lib/ollama/cuda_v12/libggml-cuda.so", Mode: 0o755}, strings.Repeat("g", 4096))
	add(&tar.Header{Name: "lib/ollama/vulkan/libggml-vulkan.so", Mode: 0o755}, "vk")
	add(&tar.Header{Name: "lib/ollama/evil", Typeflag: tar.TypeSymlink, Linkname: "../../../etc/passwd"}, "")
	add(&tar.Header{Name: "../escape", Mode: 0o644}, "no")
	tw.Close()
	var out bytes.Buffer
	zw, _ := zstd.NewWriter(&out)
	zw.Write(raw.Bytes())
	zw.Close()
	return out.Bytes()
}

func TestAIKeep(t *testing.T) {
	for name, want := range map[string]string{
		"bin/ollama":                          "ollama/bin/ollama",
		"./bin/ollama":                        "ollama/bin/ollama",
		"lib/ollama/libggml-cpu-x64.so":       "ollama/lib/ollama/libggml-cpu-x64.so",
		"lib/ollama/cuda_v13/libcublas.so.13": "",
		"lib/ollama/vulkan/libggml-vulkan.so": "ollama/lib/ollama/vulkan/libggml-vulkan.so",
		"lib/ollama/vulkan/x/y.so":            "",
		"lib/ollama/":                         "",
		"../bin/ollama":                       "ollama/bin/ollama",
		"etc/passwd":                          "",
	} {
		got, ok := aiKeep(name)
		if got != want || ok != (want != "") {
			t.Errorf("aiKeep(%q) = %q %v, want %q", name, got, ok, want)
		}
	}
}

func TestAIRecommend(t *testing.T) {
	for mem, want := range map[float64]string{3.7: "qwen3:1.7b", 7.6: "qwen3:4b", 11.5: "qwen3:4b", 15.4: "qwen3:8b", 31: "qwen3:8b"} {
		if got := aiRecommend(mem); got != want {
			t.Errorf("aiRecommend(%v) = %s, want %s", mem, got, want)
		}
	}
}

func TestAIPin(t *testing.T) {
	dir := t.TempDir()
	write := func(p aiPin) string {
		f := filepath.Join(dir, "pin.json")
		b, _ := json.Marshal(p)
		os.WriteFile(f, b, 0o644)
		return f
	}
	sum := strings.Repeat("ab", 32)
	good := "https://github.com/ollama/ollama/releases/download/v0.13.0/ollama-linux-amd64.tar.zst"
	if _, err := readAIPin(write(aiPin{"v0.13.0", good, sum})); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []aiPin{
		{"v", "http://github.com/ollama/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", "https://example.com/ollama/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", "https://github.com/someone/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", good, "abc"},
	} {
		if _, err := readAIPin(write(bad)); err == nil {
			t.Errorf("pin %+v accepted", bad)
		}
	}
	if _, err := readAIPin(filepath.Join(dir, "none.json")); err == nil {
		t.Error("missing pin accepted")
	}
}

func TestAIEngine(t *testing.T) {
	arc := fakeOllama(t)
	sum := sha256.Sum256(arc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(arc)))
		w.Write(arc)
	}))
	defer srv.Close()

	dir := t.TempDir()
	last := -1
	if err := aiEngine(dir, aiPin{URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}, func(p int) { last = p }); err != nil {
		t.Fatal(err)
	}
	if last != 100 {
		t.Errorf("progress ended at %d", last)
	}
	if !aiInstalled(dir) {
		t.Fatal("not installed")
	}
	for _, f := range []string{"lib/ollama/libggml-cpu-sandybridge.so", "lib/ollama/LLAMA_CPP_LICENSE", "lib/ollama/vulkan/libggml-vulkan.so"} {
		if _, err := os.Stat(filepath.Join(dir, "ollama", f)); err != nil {
			t.Error(err)
		}
	}
	if l, err := os.Readlink(filepath.Join(dir, "ollama/lib/ollama/libggml-base.so.0")); err != nil || l != "libggml-base.so.0.28.0" {
		t.Errorf("link %q %v", l, err)
	}
	for _, f := range []string{"ollama/lib/ollama/cuda_v12", "ollama/lib/ollama/evil", "escape", "new"} {
		if _, err := os.Lstat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s should not be there", f)
		}
	}

	// A download that does not match leaves the installed copy alone.
	other := t.TempDir()
	err := aiEngine(other, aiPin{URL: srv.URL, SHA256: strings.Repeat("00", 32)}, func(int) {})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum: %v", err)
	}
	if aiInstalled(other) {
		t.Error("installed despite a wrong checksum")
	}
	if _, err := os.Stat(filepath.Join(other, "new")); err == nil {
		t.Error("left new/ behind")
	}
}

func TestAIPull(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"status":"pulling a","digest":"sha256:a","total":1000,"completed":0}`)
		fmt.Fprintln(w, `{"status":"pulling b","digest":"sha256:b","total":10,"completed":10}`)
		fmt.Fprintln(w, `{"status":"pulling a","digest":"sha256:a","total":1000,"completed":500}`)
		fmt.Fprintln(w, `{"status":"pulling a","digest":"sha256:a","total":1000,"completed":1000}`)
		fmt.Fprintln(w, `{"status":"verifying sha256 digest"}`)
		fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer srv.Close()
	old := aiAPI
	aiAPI = srv.URL
	defer func() { aiAPI = old }()

	var steps []int
	if err := aiPull("qwen3:4b", func(p int) { steps = append(steps, p) }); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "qwen3:4b" {
		t.Errorf("asked for %v", got)
	}
	if fmt.Sprint(steps) != "[0 50 100]" {
		t.Errorf("progress %v", steps)
	}
}

func TestAIPullError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"error":"pull model manifest: file does not exist"}`)
	}))
	defer srv.Close()
	old := aiAPI
	aiAPI = srv.URL
	defer func() { aiAPI = old }()
	if err := aiPull("qwen3:4b", func(int) {}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestAIFetchRefusesUnknownModel(t *testing.T) {
	if code := aiMain([]string{"--fetch", "--model", "llama3:70b", "--dir", t.TempDir(), "--progress", filepath.Join(t.TempDir(), "p.json")}); code != 2 {
		t.Errorf("exit %d", code)
	}
}

func TestAIFetchEngineOnly(t *testing.T) {
	arc := fakeOllama(t)
	sum := sha256.Sum256(arc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(arc) }))
	defer srv.Close()
	oldOK := aiURLOK
	aiURLOK = func(string) bool { return true }
	defer func() { aiURLOK = oldOK }()

	tmp := t.TempDir()
	pin := filepath.Join(tmp, "pin.json")
	b, _ := json.Marshal(aiPin{"v-test", srv.URL, hex.EncodeToString(sum[:])})
	os.WriteFile(pin, b, 0o644)
	dir := filepath.Join(tmp, "ai")
	prog := filepath.Join(tmp, "ai.json")
	if code := aiMain([]string{"--fetch", "--dir", dir, "--pin", pin, "--progress", prog}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !aiInstalled(dir) {
		t.Fatal("not installed")
	}
	var p progress
	pb, _ := os.ReadFile(prog)
	json.Unmarshal(pb, &p)
	if !p.Done || p.Error != "" || p.Percent != 100 {
		t.Errorf("progress %+v", p)
	}
	// While one setup runs, a second one stops at once.
	lock, err := aiLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if !aiBusy(dir) {
		t.Error("not busy while locked")
	}
	if err := aiFetch(aiOpts{dir: dir, pin: pin, progress: prog}, func(string, int) {}); err == nil {
		t.Error("second setup ran")
	}
}
