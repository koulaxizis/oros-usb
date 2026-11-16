package main

// Screens on orOS USB (desk.go's settings, "display"): a second screen
// shows the same picture (duplicate), continues the first (extend), or
// only one screen is used; each screen keeps its own resolution and size
// (scale). labwc shows orOS over all its outputs; wlr-randr (labwc's
// wlr-output-management) places them:
//   - extend: side by side from left to right, the built-in screen first,
//     so orOS spans both (multitasking's side by side puts one app on each);
//   - duplicate: all at 0,0 with a resolution they share, or the same size
//     through scale when they share none;
//   - external / internal: the other screens are off. With the lid shut
//     and a screen plugged in, logind does not suspend (docked).
// The choice is applied when the session starts and whenever the set of
// screens changes (oros-system idle checks every few seconds).

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type outputPref struct {
	Res   string  `json:"res,omitempty"`
	Scale float64 `json:"scale,omitempty"`
}

type displaySettings struct {
	Mode    string                `json:"mode"`
	Outputs map[string]outputPref `json:"outputs,omitempty"`
}

var (
	displayModes  = []string{"extend", "duplicate", "external", "internal"}
	scaleChoices  = []float64{1, 1.25, 1.5, 1.75, 2}
	outputNameRe  = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	resRe         = regexp.MustCompile(`^[0-9]{3,5}x[0-9]{3,5}$`)
	wlrRandrBin   = "wlr-randr"
	maxOutputPref = 16
)

func (d displaySettings) sanitize() displaySettings {
	if !strIn(d.Mode, displayModes) {
		d.Mode = "extend"
	}
	clean := map[string]outputPref{}
	names := make([]string, 0, len(d.Outputs))
	for n := range d.Outputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := d.Outputs[n]
		if !outputNameRe.MatchString(n) || len(clean) >= maxOutputPref {
			continue
		}
		if !resRe.MatchString(p.Res) {
			p.Res = ""
		}
		ok := false
		for _, s := range scaleChoices {
			if p.Scale == s {
				ok = true
			}
		}
		if !ok {
			p.Scale = 0
		}
		if p.Res != "" || p.Scale != 0 {
			clean[n] = p
		}
	}
	d.Outputs = clean
	if len(clean) == 0 {
		d.Outputs = nil
	}
	return d
}

// wlrOutput is one screen as "wlr-randr --json" lists it.
type wlrOutput struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Make        string    `json:"make"`
	Model       string    `json:"model"`
	Enabled     bool      `json:"enabled"`
	Scale       float64   `json:"scale"`
	Modes       []wlrMode `json:"modes"`
	Position    struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"position"`
}

type wlrMode struct {
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Refresh   float64 `json:"refresh"`
	Preferred bool    `json:"preferred"`
	Current   bool    `json:"current"`
}

func (o wlrOutput) internal() bool {
	for _, p := range []string{"eDP", "LVDS", "DSI"} {
		if strings.HasPrefix(o.Name, p) {
			return true
		}
	}
	return false
}

// best is the mode with that resolution and the highest refresh, or the
// preferred one (the first when none is marked) for res "".
func (o wlrOutput) best(res string) (wlrMode, bool) {
	var m wlrMode
	found := false
	for _, x := range o.Modes {
		r := fmt.Sprintf("%dx%d", x.Width, x.Height)
		if res == "" {
			if x.Preferred && (!found || x.Refresh > m.Refresh || !m.Preferred) {
				m, found = x, true
			}
			continue
		}
		if r == res && (!found || x.Refresh > m.Refresh) {
			m, found = x, true
		}
	}
	if !found && res == "" && len(o.Modes) > 0 {
		return o.Modes[0], true
	}
	return m, found
}

// resolutions lists each size once, largest first.
func (o wlrOutput) resolutions() []string {
	seen := map[string]bool{}
	var ms []wlrMode
	for _, m := range o.Modes {
		r := fmt.Sprintf("%dx%d", m.Width, m.Height)
		if !seen[r] {
			seen[r] = true
			ms = append(ms, m)
		}
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].Width*ms[i].Height > ms[j].Width*ms[j].Height })
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = fmt.Sprintf("%dx%d", m.Width, m.Height)
	}
	return out
}

func listOutputs() ([]wlrOutput, error) {
	out, err := run(wlrRandrBin, "--json")
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	var l []wlrOutput
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		return nil, err
	}
	// Built-in screen first, then by name: the order screens extend in.
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].internal() != l[j].internal() {
			return l[i].internal()
		}
		return l[i].Name < l[j].Name
	})
	return l, nil
}

func modeArg(m wlrMode) string {
	if m.Refresh > 0 {
		return fmt.Sprintf("%dx%d@%.3fHz", m.Width, m.Height, m.Refresh)
	}
	return fmt.Sprintf("%dx%d", m.Width, m.Height)
}

func scaleArg(s float64) string { return strconv.FormatFloat(s, 'f', -1, 64) }

// displayArgs turns the settings and the screens into one wlr-randr call
// (all screens change together). nil: nothing to do.
func displayArgs(d displaySettings, outs []wlrOutput) []string {
	if len(outs) == 0 {
		return nil
	}
	mode := d.Mode
	hasInt, hasExt := false, false
	for _, o := range outs {
		if o.internal() {
			hasInt = true
		} else {
			hasExt = true
		}
	}
	// Only one kind of screen: "only the other" would leave the computer
	// dark, so all are used.
	if (mode == "external" && !hasExt) || (mode == "internal" && !hasInt) || len(outs) == 1 {
		mode = "extend"
	}
	type plan struct {
		o     wlrOutput
		m     wlrMode
		scale float64
		on    bool
	}
	var ps []plan
	for _, o := range outs {
		p := plan{o: o, on: true, scale: 1}
		if (mode == "external" && o.internal()) || (mode == "internal" && !o.internal()) {
			p.on = false
		}
		pref := d.Outputs[o.Name]
		m, ok := o.best(pref.Res)
		if !ok {
			m, ok = o.best("")
		}
		if !ok {
			continue
		}
		p.m = m
		if pref.Scale > 0 {
			p.scale = pref.Scale
		}
		ps = append(ps, p)
	}
	if mode == "duplicate" {
		// A resolution every screen has (the largest); else each keeps
		// its own and the others are scaled to the first one's size.
		common := ""
		for _, r := range ps[0].o.resolutions() {
			all := true
			for _, p := range ps[1:] {
				if !strIn(r, p.o.resolutions()) {
					all = false
				}
			}
			if all {
				common = r
				break
			}
		}
		base := ps[0]
		for i := range ps {
			if common != "" {
				ps[i].m, _ = ps[i].o.best(common)
				ps[i].scale = base.scale
				continue
			}
			if i > 0 {
				w := float64(base.m.Width) / base.scale
				ps[i].scale = math.Round(float64(ps[i].m.Width)/w*1000) / 1000
				if ps[i].scale <= 0 {
					ps[i].scale = 1
				}
			}
		}
	}
	var args []string
	x := 0
	for _, p := range ps {
		args = append(args, "--output", p.o.Name)
		if !p.on {
			args = append(args, "--off")
			continue
		}
		pos := "0,0"
		if mode != "duplicate" {
			pos = fmt.Sprintf("%d,0", x)
			x += int(math.Round(float64(p.m.Width) / p.scale))
		}
		args = append(args, "--on", "--mode", modeArg(p.m), "--scale", scaleArg(p.scale), "--pos", pos)
	}
	return args
}

func applyDisplay(d displaySettings) error {
	outs, err := listOutputs()
	if err != nil {
		return err
	}
	a := displayArgs(d, outs)
	if a == nil {
		return nil
	}
	if out, err := run(wlrRandrBin, a...); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// outputsKey names the connected screens, to notice one being plugged in
// or out.
func outputsKey() string {
	outs, err := listOutputs()
	if err != nil {
		return ""
	}
	n := make([]string, len(outs))
	for i, o := range outs {
		n[i] = o.Name
	}
	return strings.Join(n, ",")
}

// ---------- host commands ----------

type outputInfo struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Internal    bool     `json:"internal"`
	Enabled     bool     `json:"enabled"`
	Res         string   `json:"res"`
	Scale       float64  `json:"scale"`
	Resolutions []string `json:"resolutions"`
}

func displayList() reply {
	outs, err := listOutputs()
	if err != nil {
		return fail(err, "")
	}
	home, _ := os.UserHomeDir()
	d := readDesk(home)
	var l []outputInfo
	for _, o := range outs {
		i := outputInfo{Name: o.Name, Internal: o.internal(), Enabled: o.Enabled, Scale: o.Scale, Resolutions: o.resolutions()}
		i.Label = strings.TrimSpace(o.Make + " " + o.Model)
		if i.Label == "" {
			i.Label = o.Description
		}
		for _, m := range o.Modes {
			if m.Current {
				i.Res = fmt.Sprintf("%dx%d", m.Width, m.Height)
			}
		}
		l = append(l, i)
	}
	return reply{"ok": true, "outputs": l, "display": d.Display, "modes": displayModes, "scales": scaleChoices}
}

// displaySet saves the choice and applies it at once. With "cycle" it
// moves to the next mode (Ctrl+Alt+Shift+D), the way Win+P does.
func displaySet(raw json.RawMessage, cycle bool) reply {
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err, "")
	}
	d := readDesk(home)
	if cycle {
		// Without a built-in screen (a desktop with two monitors) "only
		// this / the other screen" mean nothing: only extend and duplicate.
		modes := displayModes
		if outs, err := listOutputs(); err == nil && (len(outs) == 0 || !outs[0].internal()) {
			modes = displayModes[:2]
		}
		i := -1
		for j, m := range modes {
			if m == d.Display.Mode {
				i = j
			}
		}
		d.Display.Mode = modes[(i+1)%len(modes)]
	} else if len(raw) == 0 || json.Unmarshal(raw, &d.Display) != nil {
		return reply{"ok": false, "error": "bad settings"}
	}
	d = d.sanitize()
	if err := writeDesk(home, d); err != nil {
		return fail(err, "")
	}
	if err := applyDisplay(d.Display); err != nil {
		return reply{"ok": false, "error": err.Error(), "display": d.Display}
	}
	return reply{"ok": true, "display": d.Display}
}

// displayMain is "oros-system display --apply" (oros-kiosk, at the start).
func displayMain(args []string) int {
	if len(args) != 1 || args[0] != "--apply" {
		fmt.Println("usage: oros-system display --apply")
		return 2
	}
	home, _ := os.UserHomeDir()
	if err := applyDisplay(readDesk(home).Display); err != nil {
		logf("display: %v", err)
		return 1
	}
	return 0
}
