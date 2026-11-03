package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Local AI for orOS's Assistant (System › Local AI, or Settings in the
// Assistant). The image does not carry it: like the Tor button, the first
// install downloads the official Ollama build for Linux from its GitHub
// release into the home folder (the persistence partition), checked against
// the SHA-256 that the image's build pinned (scripts/ollama.sh writes
// /opt/oros/ollama.json), and keeps only what runs on the processor (the
// graphics card libraries are most of the download and are skipped while
// it is unpacked). Then it downloads one open model (Qwen3, Apache-2.0)
// through Ollama itself.
//
// Ollama listens on 127.0.0.1:11434 only and lets in only the orOS page
// (OLLAMA_ORIGINS); orOS's Assistant talks to it like to any Ollama.
// oros-session starts it at every boot once it is installed
// ("oros-system ai --serve"); it keeps a model in memory only for a few
// minutes after its last answer.

const (
	aiAddr   = "127.0.0.1:11434"
	aiOrigin = "http://localhost:47800"
	// The download holds NVIDIA's CUDA libraries too (about 1.5 GB in
	// all); what is kept is about 120 MB.
	aiMaxDownload = 4 << 30
	aiMaxKept     = 400 << 20
)

var (
	aiPinFile = "/opt/oros/ollama.json"
	aiAPI     = "http://" + aiAddr
	aiHTTP    = &http.Client{Timeout: 2 * time.Hour}
	aiQuick   = &http.Client{Timeout: 2 * time.Second}
	// aiURLOK checks the pinned address; tests replace it.
	aiURLOK = aiURL
)

// aiModels are the models offered, smallest first, with their download
// size in MB (ollama.com/library/qwen3). qwen3:0.6b is for the CI test only
// and is not shown.
var aiModels = []struct {
	Name string
	MB   int
	Show bool
}{
	{"qwen3:0.6b", 523, false},
	{"qwen3:1.7b", 1400, true},
	{"qwen3:4b", 2500, true},
	{"qwen3:8b", 5200, true},
}

func aiModelMB(name string) (int, bool) {
	for _, m := range aiModels {
		if m.Name == name {
			return m.MB, true
		}
	}
	return 0, false
}

// aiRecommend picks the model for this much memory: the 8b needs about
// 6 GB free while it answers, the 4b about 3.5 GB. A "16 GB" computer
// reports a little less than 16 GiB.
func aiRecommend(memGiB float64) string {
	switch {
	case memGiB >= 14:
		return "qwen3:8b"
	case memGiB >= 7:
		return "qwen3:4b"
	default:
		return "qwen3:1.7b"
	}
}

func memGiB() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseFloat(f[1], 64)
			return kb / (1 << 20)
		}
	}
	return 0
}

func aiDir(home string) string          { return filepath.Join(home, ".local", "share", "oros-ai") }
func aiBin(dir string) string           { return filepath.Join(dir, "ollama", "bin", "ollama") }
func aiProgressFile(home string) string { return filepath.Join(home, "oros", "data", "ai.json") }
func aiModelsDir(dir string) string     { return filepath.Join(dir, "models") }

// aiGPU is false once the person turned the graphics card off (a card
// whose driver misbehaves); Ollama then uses the processor only.
func aiGPU(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "cpu-only"))
	return err != nil
}
func aiInstalled(dir string) bool         { _, err := os.Stat(aiBin(dir)); return err == nil }
func aiBusy(dir string) bool              { return lockBusy(filepath.Join(dir, ".lock")) }
func aiLock(dir string) (*os.File, error) { return lockTake(filepath.Join(dir, ".lock")) }

// lockBusy is true while another process holds the lock file.
func lockBusy(file string) bool {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func lockTake(file string) (*os.File, error) {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("local AI is already being set up")
	}
	return f, nil
}

// aiRunning is true when Ollama answers on its address.
func aiRunning() bool {
	resp, err := aiQuick.Get(aiAPI + "/api/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// aiList is the models Ollama has, by name.
func aiList() ([]string, error) {
	resp, err := aiQuick.Get(aiAPI + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var t struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range t.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

// aiServe starts Ollama in the background when it is installed and not
// already running. It outlives the caller (Setsid), like the browser.
func aiServe(dir string) error {
	if !aiInstalled(dir) || aiRunning() {
		return nil
	}
	cmd := exec.Command(aiBin(dir), "serve")
	cmd.Env = append(os.Environ(),
		"OLLAMA_HOST="+aiAddr,
		"OLLAMA_ORIGINS="+aiOrigin,
		"OLLAMA_MODELS="+aiModelsDir(dir),
		// One answer at a time and one model in memory: the computer stays usable.
		"OLLAMA_NUM_PARALLEL=1",
		"OLLAMA_MAX_LOADED_MODELS=1",
		// Only the models on this computer, never Ollama's cloud models.
		"OLLAMA_NO_CLOUD=1",
	)
	if !aiGPU(dir) {
		cmd.Env = append(cmd.Env, "OLLAMA_VULKAN=0")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("ai-serve.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 60; i++ {
		if aiRunning() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("the local AI did not start (see ai-serve.log)")
}

// aiStop ends every Ollama process started from this folder.
func aiStop(dir string) {
	bin := aiBin(dir)
	procs, _ := filepath.Glob("/proc/[0-9]*/exe")
	var pids []int
	for _, p := range procs {
		if exe, err := os.Readlink(p); err == nil && (exe == bin || exe == bin+" (deleted)") {
			if pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p))); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	for i := 0; i < 20 && len(pids) > 0 && aiRunning(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func freeMB(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize) >> 20
}

// --- the native host's commands ---

func aiStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := aiDir(home)
	_ = os.MkdirAll(dir, 0o700)
	mem := memGiB()
	r := readProgress(aiProgressFile(home))
	r["running"] = aiBusy(dir)
	r["installed"] = aiInstalled(dir)
	r["persistent"] = persistenceTarget() != ""
	r["memGiB"] = mem
	r["recommended"] = aiRecommend(mem)
	r["freeMB"] = freeMB(dir)
	r["address"] = aiAPI
	r["gpu"] = aiGPU(dir)
	var offer []reply
	for _, m := range aiModels {
		if m.Show {
			offer = append(offer, reply{"name": m.Name, "mb": m.MB})
		}
	}
	r["offer"] = offer
	_, hasPin := readAIPin(aiPinFile)
	r["available"] = hasPin == nil
	if aiRunning() {
		r["serving"] = true
		if l, err := aiList(); err == nil {
			r["models"] = l
		}
	}
	return r
}

// aiInstall is "ai.install": the engine (when missing) and the model, in
// the background; ai.status follows it.
func aiInstall(model string) reply {
	if _, ok := aiModelMB(model); !ok {
		return reply{"ok": false, "error": "unknown model"}
	}
	if persistenceTarget() == "" {
		return reply{"ok": false, "error": "no persistence"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := aiDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err, "")
	}
	if aiBusy(dir) {
		return reply{"ok": true, "downloading": true}
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err, "")
	}
	_ = os.Remove(aiProgressFile(home))
	cmd := exec.Command(self, "ai", "--fetch", "--model", model)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f := logFile("ai.log"); f != nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fail(err, "")
	}
	go func() { _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	return reply{"ok": true, "downloading": true}
}

// aiStart is "ai.start": Ollama again after it was stopped or crashed.
func aiStart() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if err := aiServe(aiDir(home)); err != nil {
		return fail(err, "")
	}
	return reply{"ok": true}
}

// aiSetGPU is "ai.gpu": the graphics card on or off, then Ollama again.
func aiSetGPU(on bool) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := aiDir(home)
	lock, err := aiLock(dir)
	if err != nil {
		return fail(errors.New("busy"), "")
	}
	defer lock.Close()
	marker := filepath.Join(dir, "cpu-only")
	if on {
		err = os.Remove(marker)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else {
		err = os.WriteFile(marker, nil, 0o600)
	}
	if err != nil {
		return fail(err, "")
	}
	if aiInstalled(dir) {
		aiStop(dir)
		if err := aiServe(dir); err != nil {
			return fail(err, "")
		}
	}
	return reply{"ok": true}
}

// aiForget is "ai.forget": one model goes, the engine stays.
func aiForget(model string) reply {
	if _, ok := aiModelMB(model); !ok {
		return reply{"ok": false, "error": "unknown model"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if aiBusy(aiDir(home)) {
		return reply{"ok": false, "error": "busy"}
	}
	b, _ := json.Marshal(map[string]string{"model": model})
	req, _ := http.NewRequest(http.MethodDelete, aiAPI+"/api/delete", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := aiQuick.Do(req)
	if err != nil {
		return fail(err, "")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return reply{"ok": false, "error": resp.Status}
	}
	return reply{"ok": true}
}

// aiRemove is "ai.remove": Ollama stops, and it goes with every model.
func aiRemove() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	dir := aiDir(home)
	lock, err := aiLock(dir)
	if err != nil {
		return fail(errors.New("busy"), "")
	}
	defer lock.Close()
	aiStop(dir)
	for _, d := range []string{"ollama", "models", "new"} {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return fail(err, "")
		}
	}
	_ = os.Remove(aiProgressFile(home))
	return reply{"ok": true}
}

// --- oros-system ai ---

type aiPin struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// readAIPin reads the build's pin of the Ollama release; only GitHub
// release downloads of ollama/ollama are accepted.
func readAIPin(file string) (aiPin, error) {
	var p aiPin
	b, err := os.ReadFile(file)
	if err != nil {
		return p, fmt.Errorf("this image has no local AI download (%v)", err)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("%s: %v", file, err)
	}
	if !aiURLOK(p.URL) {
		return p, fmt.Errorf("unexpected download address %q", p.URL)
	}
	if s, err := hex.DecodeString(p.SHA256); err != nil || len(s) != sha256.Size {
		return p, errors.New("the local AI download has no valid SHA-256")
	}
	return p, nil
}

func aiURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" &&
		strings.HasPrefix(u.Path, "/ollama/ollama/releases/download/") && strings.HasSuffix(u.Path, ".tar.zst")
}

type aiOpts struct {
	dir, pin, progress, model string
}

func aiMain(args []string) int {
	fl := flag.NewFlagSet("ai", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	o := aiOpts{}
	var fetch, serve, stop bool
	fl.BoolVar(&fetch, "fetch", false, "download and check Ollama (when missing), then the model")
	fl.BoolVar(&serve, "serve", false, "start Ollama in the background when it is installed")
	fl.BoolVar(&stop, "stop", false, "stop Ollama")
	fl.StringVar(&o.model, "model", "", "model to download (qwen3:1.7b, qwen3:4b, qwen3:8b)")
	fl.StringVar(&o.dir, "dir", aiDir(home), "folder for Ollama and its models")
	fl.StringVar(&o.pin, "pin", aiPinFile, "the build's pin of the Ollama release (JSON)")
	fl.StringVar(&o.progress, "progress", aiProgressFile(home), "progress file (JSON)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if stop {
		aiStop(o.dir)
		return 0
	}
	if serve {
		if err := aiServe(o.dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if !fetch {
		fmt.Fprintln(os.Stderr, "usage: oros-system ai --fetch [--model qwen3:4b] [--dir D] | ai --serve | ai --stop")
		return 2
	}
	if o.model != "" {
		if _, ok := aiModelMB(o.model); !ok {
			fmt.Fprintln(os.Stderr, "unknown model", o.model)
			return 2
		}
	}
	report := progressWriter(o.progress)
	if err := aiFetch(o, func(step string, pct int) { report(progress{Step: step, Percent: pct}) }); err != nil {
		report(progress{Step: "error", Done: true, Error: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(progress{Step: "done", Percent: 100, Done: true})
	return 0
}

func aiFetch(o aiOpts, step func(string, int)) error {
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	lock, err := aiLock(o.dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	need := int64(0)
	if !aiInstalled(o.dir) {
		need += 200
	}
	if o.model != "" {
		mb, _ := aiModelMB(o.model)
		need += int64(mb) + 1024
	}
	if free := freeMB(o.dir); free >= 0 && free < need {
		return fmt.Errorf("not enough free space: %d MB needed, %d MB free", need, free)
	}
	share := 0
	if !aiInstalled(o.dir) {
		pin, err := readAIPin(o.pin)
		if err != nil {
			return err
		}
		fmt.Printf("Ollama %s: %s\n", pin.Version, pin.URL)
		end := 100
		if o.model != "" {
			end = 30
		}
		if err := aiEngine(o.dir, pin, func(pct int) { step("engine", pct*end/100) }); err != nil {
			return err
		}
		share = end
	}
	if o.model == "" {
		return nil
	}
	step("start", share)
	if err := aiServe(o.dir); err != nil {
		return err
	}
	return aiPull(o.model, func(pct int) { step("model", share+pct*(99-share)/100) })
}

// aiKeep says which entries of the Ollama archive are kept, and where
// (under "ollama"): the program and the libraries next to it (the
// processor variants for old and new processors among them) and the
// Vulkan library for graphics cards, not the CUDA (NVIDIA's own driver)
// folders, which are most of the download and cannot run here.
func aiKeep(name string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "bin/ollama" {
		return "ollama/" + name, true
	}
	if rest, ok := strings.CutPrefix(name, "lib/ollama/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "ollama/" + name, true
	}
	// Vulkan reaches most graphics cards through the image's own Mesa
	// drivers (AMD, Intel, newer NVIDIA), with no vendor driver.
	if rest, ok := strings.CutPrefix(name, "lib/ollama/vulkan/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "ollama/" + name, true
	}
	return "", false
}

// aiEngine streams the archive: its SHA-256 is computed while it is
// unpacked into "new", and only a matching download takes the place of
// "ollama". The full archive is never kept on the disk.
func aiEngine(dir string, pin aiPin, pct func(int)) error {
	resp, err := aiHTTP.Get(pin.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", pin.URL, resp.Status)
	}
	if resp.ContentLength > aiMaxDownload {
		return fmt.Errorf("%s: too large", pin.URL)
	}
	h := sha256.New()
	counted := &countReader{r: io.LimitReader(resp.Body, aiMaxDownload+1), total: resp.ContentLength, pct: pct}
	body := io.TeeReader(counted, h)
	dec, err := zstd.NewReader(body, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer dec.Close()

	tmp := filepath.Join(dir, "new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var kept int64
	tr := tar.NewReader(dec)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("unpacking Ollama: %v", err)
		}
		name, ok := aiKeep(hd.Name)
		if !ok {
			continue
		}
		dst := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		switch hd.Typeflag {
		case tar.TypeReg:
			kept += hd.Size
			if kept > aiMaxKept {
				return errors.New("the Ollama download is larger than expected")
			}
			mode := os.FileMode(0o644)
			if hd.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, hd.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only links to a file in the same folder.
			target := strings.TrimPrefix(hd.Linkname, "./")
			if target == "" || strings.Contains(target, "/") || target == ".." {
				continue
			}
			if err := os.Symlink(target, dst); err != nil {
				return err
			}
		}
	}
	// The rest of the stream counts for the checksum too.
	if _, err := io.Copy(io.Discard, dec); err != nil {
		return fmt.Errorf("unpacking Ollama: %v", err)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	if counted.n > aiMaxDownload {
		return fmt.Errorf("%s: too large", pin.URL)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, pin.SHA256) {
		return fmt.Errorf("the Ollama download does not match its checksum (got %s)", got)
	}
	if fi, err := os.Stat(aiBin(tmp)); err != nil || fi.Mode()&0o111 == 0 {
		return errors.New("the Ollama download has an unexpected layout")
	}
	cur := filepath.Join(dir, "ollama")
	_ = os.RemoveAll(cur)
	return os.Rename(filepath.Join(tmp, "ollama"), cur)
}

type countReader struct {
	r     io.Reader
	n     int64
	total int64
	pct   func(int)
	last  int
}

func (c *countReader) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n += int64(k)
	if c.total > 0 && c.pct != nil {
		if v := int(c.n * 100 / c.total); v != c.last && v <= 100 {
			c.last = v
			c.pct(v)
		}
	}
	return k, err
}

// aiPull downloads a model through Ollama (from its own registry, where
// Ollama checks every layer's digest) and follows its progress.
func aiPull(model string, pct func(int)) error {
	b, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	resp, err := aiHTTP.Post(aiAPI+"/api/pull", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("model download: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	// The largest layer is the model's weights; its share is the progress.
	var big string
	var bigTotal int64
	for sc.Scan() {
		var m struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Error != "" {
			return fmt.Errorf("model download: %s", m.Error)
		}
		if m.Status == "success" {
			return nil
		}
		if m.Total > bigTotal {
			big, bigTotal = m.Digest, m.Total
		}
		if m.Digest == big && bigTotal > 0 {
			pct(int(m.Completed * 100 / bigTotal))
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("model download: %v", err)
	}
	return errors.New("model download ended early")
}
