package main

// Console mode (wave G7): the computer becomes a games console, for a
// laptop on the television with a controller. Steam's Big Picture (its
// controller interface) runs alone on the screen in gamescope, Valve's
// compositor for games (the one the Steam Deck uses), when the system has
// it (Debian 13 has no package), otherwise in labwc, instead of orOS.
// Leaving Steam (Power › Exit Steam) brings orOS back.
//
// The System panel's Software section starts it ("console.start": the
// request file, then labwc ends) and can make every start of the computer
// begin in it ("console.boot", the owner's choice, ~/oros/data/console.json).
// oros-session runs "oros-system console --run" on tty1 in place of labwc.
// When gamescope is missing or cannot start on this graphics card (some
// old cards, virtual machines), Steam's Big Picture runs in labwc.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	steamID          = "com.valvesoftware.Steam"
	consoleLabwcConf = "/etc/xdg/oros-labwc"
)

// gamescope failing sooner than this counts as "cannot start here".
var consoleQuickFail = 20 * time.Second

type consoleSettings struct {
	Boot bool `json:"boot"`
}

func consoleFile(home string) string { return filepath.Join(home, "oros", "data", "console.json") }

func readConsole(home string) consoleSettings {
	var s consoleSettings
	if b, err := os.ReadFile(consoleFile(home)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func writeConsole(home string, s consoleSettings) error {
	f := consoleFile(home)
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	if err := os.WriteFile(f+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(f+".tmp", f)
}

func steamInstalled() bool {
	_, ok := flatpakInstalled()[steamID]
	return ok
}

// consoleSteam is the command that starts Steam's Big Picture.
func consoleSteam() []string { return []string{flatpakBin, "run", steamID, "-gamepadui"} }

// consoleGamescope: gamescope's arguments. -e lets Steam steer it (which
// window shows, the frame limit and scaling from Steam's quick menu).
func consoleGamescope() []string { return append([]string{"-e", "--"}, consoleSteam()...) }

// consoleLabwc: the fallback, Big Picture as the only window of labwc's
// kiosk set-up; labwc ends when Steam does.
func consoleLabwc() []string {
	q := make([]string, 0, 4)
	for _, a := range consoleSteam() {
		q = append(q, shellQuote(a))
	}
	return []string{"-C", consoleLabwcConf, "-S", strings.Join(q, " ")}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ---------- the native host's side ----------

func consoleStatus() reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	_, gerr := exec.LookPath("gamescope")
	return reply{"ok": true, "steam": steamInstalled(), "gamescope": gerr == nil, "boot": readConsole(home).Boot}
}

// consoleStart is "console.start": the request for oros-session, then
// labwc ends (as for a keyboard change) and the console begins.
func consoleStart() reply {
	req := os.Getenv("OROS_CONSOLE")
	if req == "" {
		return reply{"ok": false, "error": "not in the orOS session"}
	}
	if !steamInstalled() {
		return reply{"ok": false, "error": "steam missing"}
	}
	if err := os.WriteFile(req, []byte("1\n"), 0o600); err != nil {
		return fail(err, "")
	}
	return sessionRestart()
}

// consoleSetBoot is "console.boot" (owner only: how the computer starts).
func consoleSetBoot(on bool) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	if err := writeConsole(home, consoleSettings{Boot: on}); err != nil {
		return fail(err, "")
	}
	return consoleStatus()
}

// ---------- oros-session's side ----------

// consoleMain is "oros-system console --run | --boot-wanted".
func consoleMain(args []string) int {
	fl := flag.NewFlagSet("console", flag.ContinueOnError)
	var runIt, bootWanted bool
	fl.BoolVar(&runIt, "run", false, "run Steam's Big Picture on this screen until Steam ends")
	fl.BoolVar(&bootWanted, "boot-wanted", false, "exit 0 when the computer should start in console mode")
	if err := fl.Parse(args); err != nil || runIt == bootWanted {
		fmt.Fprintln(os.Stderr, "usage: oros-system console --run | --boot-wanted")
		return 2
	}
	if bootWanted {
		home, err := os.UserHomeDir()
		if err != nil || !readConsole(home).Boot || !steamInstalled() {
			return 1
		}
		return 0
	}
	if err := consoleRun(); err != nil {
		fmt.Fprintln(os.Stderr, "console: "+err.Error())
		return 1
	}
	return 0
}

func consoleRun() error {
	if !steamInstalled() {
		return errors.New("Steam is not installed")
	}
	if _, err := exec.LookPath("gamescope"); err == nil {
		start := time.Now()
		err := attached("gamescope", consoleGamescope()...)
		if err == nil || time.Since(start) >= consoleQuickFail {
			return nil
		}
		fmt.Fprintf(os.Stderr, "console: gamescope ended after %s (%v); Big Picture in labwc\n", time.Since(start).Round(time.Second), err)
	}
	return attached("labwc", consoleLabwc()...)
}

// attached runs a program on the session's terminal and log.
func attached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
