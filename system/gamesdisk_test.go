package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGamesCandidate(t *testing.T) {
	big := int64(500 << 30)
	for _, c := range []struct {
		v  volume
		ok bool
	}{
		{volume{FSType: "ext4", UUID: "a1", Size: big}, true},
		{volume{FSType: "btrfs", UUID: "a1", Size: big}, true},
		{volume{FSType: "xfs", UUID: "a1", Size: big}, true},
		{volume{FSType: "ntfs", UUID: "a1", Size: big}, false},
		{volume{FSType: "exfat", UUID: "a1", Size: big}, false},
		{volume{FSType: "vfat", UUID: "a1", Size: big}, false},
		{volume{FSType: "ext4", Size: big}, false},
		{volume{FSType: "ext4", UUID: "a1", Size: 8 << 30}, false},
	} {
		if err := gamesCandidate(c.v); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.v, err)
		}
	}
}

// The disk is remembered by UUID: lsblk's column reaches the volume.
func TestPickVolumesUUID(t *testing.T) {
	j := `{"blockdevices":[
	{"name":"sdb","path":"/dev/sdb","pkname":null,"type":"disk","size":500107862016,"fstype":null,"label":null,"uuid":null,"mountpoint":null,"rm":false,"hotplug":true,"tran":"usb","model":"Game SSD"},
	{"name":"sdb1","path":"/dev/sdb1","pkname":"sdb","type":"part","size":500106813440,"fstype":"ext4","label":"GAMES","uuid":"6f1c9a8e-3b0d-4c55-9f0e-2a7d1b9e4c11","mountpoint":"/media/oros/games","rm":false,"hotplug":true,"tran":null,"model":null}
	]}`
	vs, err := pickVolumes(j, map[string]bool{})
	if err != nil || len(vs) != 1 {
		t.Fatal(vs, err)
	}
	if vs[0].UUID != "6f1c9a8e-3b0d-4c55-9f0e-2a7d1b9e4c11" || vs[0].Mount != "/media/oros/games" || gamesCandidate(vs[0]) != nil {
		t.Fatalf("%+v", vs[0])
	}
}

func TestReadGamesState(t *testing.T) {
	old := gamesStateFile
	defer func() { gamesStateFile = old }()
	gamesStateFile = filepath.Join(t.TempDir(), "g.json")
	if _, ok := readGamesState(); ok {
		t.Fatal("no file, but a state")
	}
	if err := os.WriteFile(gamesStateFile, []byte(`{"uuid":"x","fstype":"ext4"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, ok := readGamesState(); !ok || s.UUID != "x" {
		t.Fatal(s, ok)
	}
}
