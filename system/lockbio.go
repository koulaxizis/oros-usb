package main

// orOS's screen lock with a fingerprint (LK-BIO, koulaxizis/oros lock.js;
// the same commands as the orOS Portable launcher's lockbio.go):
//
//   {cmd:"bio.state"}            → {can, kind:"finger", kids}
//   {cmd:"bio.seal", kid}        → one touch, then a new 32-byte secret D
//   {cmd:"bio.open", kid}        → one touch, then D for kid
//   {cmd:"bio.clear", kid}       → D for kid removed
//
// fprintd's tools ("fprintd-list", "fprintd-verify", any enrolled finger)
// run as the session's user; polkit lets the active local session verify.
// The page keeps a token wrapped with D, so the lock opens only with a D
// given after a touch. D lies in ~/oros/lock-bio.json, readable only by
// this user; on a stick with encryption (LUKS) it is encrypted with the
// rest. As the lock itself, it protects the screen, not the disk.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const bioMaxKids = 16

var bioKidRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// Replaced by tests.
var (
	fprintRun = func(ctx context.Context, name string, args ...string) (string, error) {
		c := exec.CommandContext(ctx, name, args...)
		c.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
		out, err := c.CombinedOutput()
		return string(out), err
	}
	fprintLook = exec.LookPath
	bioPathFn  = func() string {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "oros", "lock-bio.json")
	}
)

type bioStoreFile struct {
	V    int               `json:"v"`
	Kids map[string]string `json:"kids"`
}

func bioLoad() bioStoreFile {
	s := bioStoreFile{V: 1, Kids: map[string]string{}}
	b, err := os.ReadFile(bioPathFn())
	if err != nil {
		return s
	}
	var in bioStoreFile
	if json.Unmarshal(b, &in) != nil || in.V != 1 {
		return s
	}
	for k, v := range in.Kids {
		if d, err := base64.StdEncoding.DecodeString(v); err == nil && len(d) == 32 && bioKidRe.MatchString(k) && len(s.Kids) < bioMaxKids {
			s.Kids[k] = v
		}
	}
	return s
}

func bioSave(s bioStoreFile) error {
	p := bioPathFn()
	if len(s.Kids) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func bioKind() string {
	if _, err := fprintLook("fprintd-verify"); err != nil {
		return ""
	}
	if _, err := fprintLook("fprintd-list"); err != nil {
		return ""
	}
	return "finger"
}

func fprintUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// fprintCan reads fprintd-list's answer (LANG=C).
func fprintCan(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "no devices available"), strings.Contains(low, "impossible to"):
		return "no-hw"
	case strings.Contains(low, "#0:"), strings.Contains(low, " - #"):
		return "ok"
	case strings.Contains(low, "no fingers enrolled"), strings.Contains(low, "fingerprints for user"):
		return "none"
	}
	return "no-hw"
}

// fprintVerdict reads fprintd-verify's answer: "" on a match, else the code.
func fprintVerdict(out string, err error, timedOut bool) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "verify-match") && err == nil:
		return ""
	case timedOut:
		return "timeout"
	case strings.Contains(low, "no devices available"):
		return "no-hw"
	case strings.Contains(low, "no fingers enrolled"), strings.Contains(low, "verify-no-enrolled"):
		return "none"
	case strings.Contains(low, "already in use"), strings.Contains(low, "device was already claimed"):
		return "busy"
	}
	return "failed"
}

func fprintVerify() string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := fprintRun(ctx, "fprintd-verify")
	return fprintVerdict(out, err, errors.Is(ctx.Err(), context.DeadlineExceeded))
}

func bioNo(code string) reply { return reply{"ok": false, "error": code} }

func bioHost(req request) reply {
	if req.Cmd == "bio.state" {
		c, k := "no-hw", bioKind()
		if k != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			out, _ := fprintRun(ctx, "fprintd-list", fprintUser())
			cancel()
			c = fprintCan(out)
		}
		kids := []string{}
		for kid := range bioLoad().Kids {
			kids = append(kids, kid)
		}
		sort.Strings(kids)
		return reply{"ok": true, "can": c, "kind": k, "kids": kids}
	}
	if !bioKidRe.MatchString(req.Kid) {
		return bioNo("bad")
	}
	s := bioLoad()
	switch req.Cmd {
	case "bio.clear":
		delete(s.Kids, req.Kid)
		if err := bioSave(s); err != nil {
			return bioNo("failed")
		}
		return reply{"ok": true}
	case "bio.seal", "bio.open":
		have, has := s.Kids[req.Kid]
		if req.Cmd == "bio.open" && !has {
			return bioNo("none")
		}
		if req.Cmd == "bio.seal" && !has && len(s.Kids) >= bioMaxKids {
			return bioNo("full")
		}
		if bioKind() == "" {
			return bioNo("no-hw")
		}
		if code := fprintVerify(); code != "" {
			return bioNo(code)
		}
		if req.Cmd == "bio.open" {
			return reply{"ok": true, "d": have}
		}
		d := make([]byte, 32)
		if _, err := rand.Read(d); err != nil {
			return bioNo("failed")
		}
		s.Kids[req.Kid] = base64.StdEncoding.EncodeToString(d)
		if err := bioSave(s); err != nil {
			return bioNo("failed")
		}
		return reply{"ok": true, "d": s.Kids[req.Kid]}
	}
	return bioNo("bad")
}
