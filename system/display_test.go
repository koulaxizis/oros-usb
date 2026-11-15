package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func outs(t *testing.T, js string) []wlrOutput {
	var l []wlrOutput
	if err := json.Unmarshal([]byte(js), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

const laptopAndTV = `[
 {"name":"eDP-1","enabled":true,"scale":1,"modes":[{"width":1920,"height":1080,"refresh":60.0,"preferred":true,"current":true},{"width":1280,"height":720,"refresh":60.0}]},
 {"name":"HDMI-A-1","enabled":true,"scale":1,"modes":[{"width":3840,"height":2160,"refresh":30.0,"preferred":true},{"width":1920,"height":1080,"refresh":50.0},{"width":1920,"height":1080,"refresh":60.0},{"width":1280,"height":720,"refresh":60.0}]}
]`

func TestDisplayExtend(t *testing.T) {
	d := displaySettings{Mode: "extend", Outputs: map[string]outputPref{"HDMI-A-1": {Scale: 2}}}.sanitize()
	got := strings.Join(displayArgs(d, outs(t, laptopAndTV)), " ")
	want := "--output eDP-1 --on --mode 1920x1080@60.000Hz --scale 1 --pos 0,0 --output HDMI-A-1 --on --mode 3840x2160@30.000Hz --scale 2 --pos 1920,0"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestDisplayDuplicateCommonResolution(t *testing.T) {
	got := strings.Join(displayArgs(displaySettings{Mode: "duplicate"}, outs(t, laptopAndTV)), " ")
	want := "--output eDP-1 --on --mode 1920x1080@60.000Hz --scale 1 --pos 0,0 --output HDMI-A-1 --on --mode 1920x1080@60.000Hz --scale 1 --pos 0,0"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestDisplayDuplicateNoCommonResolution(t *testing.T) {
	l := outs(t, `[{"name":"eDP-1","modes":[{"width":1600,"height":900,"refresh":60,"preferred":true}]},
	 {"name":"DP-1","modes":[{"width":2560,"height":1440,"refresh":60,"preferred":true}]}]`)
	got := strings.Join(displayArgs(displaySettings{Mode: "duplicate"}, l), " ")
	if !strings.Contains(got, "--output DP-1 --on --mode 2560x1440@60.000Hz --scale 1.6 --pos 0,0") {
		t.Fatalf("got %s", got)
	}
}

func TestDisplayOnlyOneScreen(t *testing.T) {
	got := strings.Join(displayArgs(displaySettings{Mode: "external"}, outs(t, laptopAndTV)), " ")
	if !strings.HasPrefix(got, "--output eDP-1 --off --output HDMI-A-1 --on") {
		t.Fatalf("external: %s", got)
	}
	got = strings.Join(displayArgs(displaySettings{Mode: "internal"}, outs(t, laptopAndTV)), " ")
	if !strings.Contains(got, "--output HDMI-A-1 --off") || strings.Contains(got, "eDP-1 --off") {
		t.Fatalf("internal: %s", got)
	}
	// A desktop with one monitor never goes dark, whatever was saved.
	one := outs(t, `[{"name":"HDMI-A-1","modes":[{"width":1920,"height":1080,"refresh":60,"preferred":true}]}]`)
	if got := strings.Join(displayArgs(displaySettings{Mode: "internal"}, one), " "); strings.Contains(got, "--off") {
		t.Fatalf("one screen: %s", got)
	}
}

func TestDisplaySanitize(t *testing.T) {
	d := displaySettings{Mode: "rm -rf", Outputs: map[string]outputPref{
		"HDMI-A-1": {Res: "1920x1080", Scale: 1.5}, "x;y": {Res: "1x1"}, "DP-2": {Res: "big", Scale: 9}}}.sanitize()
	if d.Mode != "extend" || len(d.Outputs) != 1 || d.Outputs["HDMI-A-1"].Scale != 1.5 {
		t.Fatalf("%+v", d)
	}
	// An unknown resolution falls back on the screen's preferred one.
	got := strings.Join(displayArgs(displaySettings{Mode: "extend", Outputs: map[string]outputPref{"eDP-1": {Res: "800x600"}}}, outs(t, laptopAndTV)), " ")
	if !strings.Contains(got, "eDP-1 --on --mode 1920x1080@60.000Hz") {
		t.Fatalf("%s", got)
	}
}
