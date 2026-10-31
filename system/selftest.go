package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// selftestFilesMain is "oros-system selftest-files", run once by
// oros-selftest-files.service in boot tests only: it uses the launcher's
// computer files API the way orOS Files does (the page's cookie, the
// same-origin headers) against the test's USB stick labelled OROSTEST,
// and writes what happened to ~/.oros-selftest-files for test-boot.sh.
func selftestFilesMain() int {
	home, _ := os.UserHomeDir()
	var log bytes.Buffer
	err := selftestFiles(&log, "http://localhost:47800", "OROSTEST", 4*time.Minute)
	if err != nil {
		fmt.Fprintf(&log, "FAIL: %v\n", err)
	} else {
		fmt.Fprintln(&log, "PASS")
	}
	_ = os.WriteFile(filepath.Join(home, ".oros-selftest-files"), log.Bytes(), 0o644)
	if err != nil {
		return 1
	}
	return 0
}

func selftestFiles(log io.Writer, base, label string, wait time.Duration) error {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 60 * time.Second}
	// The launcher sets the key on the orOS page; wait for the session.
	deadline := time.Now().Add(wait)
	for {
		r, err := c.Get(base + "/")
		if err == nil {
			r.Body.Close()
			u, _ := url.Parse(base + "/__oros/fs/")
			if r.StatusCode == 200 && len(jar.Cookies(u)) > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the launcher gave no computer files key (is OROS_COMPUTER_FILES set?)")
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Fprintln(log, "key: ok")
	call := func(method, path string, body []byte, out any) error {
		req, _ := http.NewRequest(method, base+"/__oros/fs/"+path, bytes.NewReader(body))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-Oros-FS", "1")
		r, err := c.Do(req)
		if err != nil {
			return err
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(log, "%s %s: %d %s\n", method, path, r.StatusCode, strings.TrimSpace(string(b)))
		if r.StatusCode != 200 {
			return fmt.Errorf("%s %s: %d", method, path, r.StatusCode)
		}
		if out != nil {
			return json.Unmarshal(b, out)
		}
		return nil
	}
	op := func(m map[string]string, out any) error {
		b, _ := json.Marshal(m)
		return call("POST", "op", b, out)
	}
	var vols []volume
	if err := call("GET", "disks", nil, &vols); err != nil {
		return err
	}
	dev := ""
	for _, v := range vols {
		if v.Label == label {
			dev = v.Path
		}
	}
	if dev == "" {
		return fmt.Errorf("no disk labelled %s in the list", label)
	}
	var m struct{ Path string }
	if err := op(map[string]string{"op": "mount", "dev": dev}, &m); err != nil {
		return err
	}
	if m.Path != "/disks/"+label {
		return fmt.Errorf("mounted at %q", m.Path)
	}
	if err := call("PUT", "raw?path="+url.QueryEscape(m.Path+"/hello.txt"), []byte("orOS computer files"), nil); err != nil {
		return err
	}
	var l []struct{ Name string }
	if err := call("GET", "list?path="+url.QueryEscape(m.Path), nil, &l); err != nil {
		return err
	}
	found := false
	for _, e := range l {
		found = found || e.Name == "hello.txt"
	}
	if !found {
		return fmt.Errorf("hello.txt not listed on the stick")
	}
	var roots struct {
		Disks []struct {
			Path     string
			ReadOnly bool
		}
	}
	if err := call("GET", "roots", nil, &roots); err != nil {
		return err
	}
	for _, d := range roots.Disks {
		if d.Path == m.Path && d.ReadOnly {
			return fmt.Errorf("the stick opened read-only")
		}
	}
	if err := call("PUT", "raw?path="+url.QueryEscape("/home/Documents/selftest.txt"), []byte("x"), nil); err != nil {
		return err
	}
	if err := op(map[string]string{"op": "trash", "path": "/home/Documents/selftest.txt"}, nil); err != nil {
		return err
	}
	if err := op(map[string]string{"op": "unmount", "dev": dev}, nil); err != nil {
		return err
	}
	return nil
}
