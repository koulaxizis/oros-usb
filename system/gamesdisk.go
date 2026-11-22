package main

// The game library on another disk (wave G6). Games take tens of
// gigabytes; a stick's persistence partition is small. The owner picks a
// Linux-formatted disk in Settings › Software, and from then on root opens
// it at every session start at one fixed place, /media/oros/games, with
// programs allowed to run from it (other disks open with noexec). The game
// stores from the software center may write there (a Flatpak override),
// and its "Games" folder belongs to the person: in Steam it is added once
// under Settings › Storage, in Heroic and Lutris as the install folder.
//
// Only ext4, ext3, btrfs and xfs: Proton's Windows folders need Linux
// permissions and symbolic links, which FAT, exFAT and NTFS do not keep.
// The choice is kept by its filesystem UUID in /home/.oros-games.json
// (root's, on the persistence partition), so the disk is found again
// whatever name the kernel gives it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

var (
	gamesMount     = "/media/oros/games"
	gamesStateFile = "/home/.oros-games.json"
)

const gamesFolder = "Games"

var gamesFS = map[string]bool{"ext4": true, "ext3": true, "btrfs": true, "xfs": true}

type gamesState struct {
	UUID   string `json:"uuid"`
	Label  string `json:"label,omitempty"`
	FSType string `json:"fstype"`
}

func readGamesState() (gamesState, bool) {
	var s gamesState
	b, err := os.ReadFile(gamesStateFile)
	if err != nil || json.Unmarshal(b, &s) != nil || s.UUID == "" {
		return s, false
	}
	return s, true
}

// gamesCandidate: a disk that can hold the library.
func gamesCandidate(v volume) error {
	if !gamesFS[v.FSType] {
		return fmt.Errorf("%s disks cannot hold Windows games (Linux formats only: ext4, btrfs, xfs)", v.FSType)
	}
	if v.UUID == "" {
		return errors.New("this disk has no UUID")
	}
	if v.Size < 16<<30 {
		return errors.New("this disk is smaller than 16 GB")
	}
	return nil
}

// gamesDiskMain is root's part ("oros-system mount --games-set DEV",
// "--games-off", "--games-boot"; the Disks sudo rule).
func gamesDiskMain(dev string, off bool) int {
	err := func() error {
		switch {
		case off:
			if isMountpoint(gamesMount) {
				if out, err := run("umount", gamesMount); err != nil {
					return cmdErr("umount", out, err)
				}
			}
			_ = os.Remove(gamesMount)
			if err := os.Remove(gamesStateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		case dev != "":
			v, err := findVolume(dev)
			if err != nil {
				return err
			}
			if err := gamesCandidate(v); err != nil {
				return err
			}
			if v.Mount != "" && v.Mount != gamesMount {
				if err := unmountVolume(v); err != nil {
					return err
				}
			}
			if err := gamesOpen(v); err != nil {
				return err
			}
			b, _ := json.Marshal(gamesState{UUID: v.UUID, Label: v.Label, FSType: v.FSType})
			if err := os.WriteFile(gamesStateFile+".tmp", b, 0o644); err != nil {
				return err
			}
			return os.Rename(gamesStateFile+".tmp", gamesStateFile)
		default: // --games-boot
			s, ok := readGamesState()
			if !ok || isMountpoint(gamesMount) {
				return nil
			}
			vs, err := listVolumes()
			if err != nil {
				return err
			}
			for _, v := range vs {
				if v.UUID == s.UUID && gamesCandidate(v) == nil {
					if v.Mount != "" {
						if err := unmountVolume(v); err != nil {
							return err
						}
					}
					return gamesOpen(v)
				}
			}
			return nil // not plugged in now
		}
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	return 0
}

// gamesOpen mounts the disk at gamesMount, writable and with programs
// allowed, and gives the person its Games folder.
func gamesOpen(v volume) error {
	if err := os.MkdirAll(gamesMount, 0o755); err != nil {
		return err
	}
	if !isMountpoint(gamesMount) {
		opts := "rw,nosuid,nodev"
		if out, err := run("mount", "-t", v.FSType, "-o", opts, v.Path, gamesMount); err != nil {
			_ = os.Remove(gamesMount)
			return cmdErr("mount", out, err)
		}
	}
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		name = "oros"
	}
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir := filepath.Join(gamesMount, gamesFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.Lchown(dir, uid, gid)
}

// --- the native host's side ---

// gamesDiskStatus is "games.disk": the chosen disk, whether it is open,
// and the disks that could hold the library.
func gamesDiskStatus() reply {
	r := reply{"ok": true, "path": filepath.Join(gamesMount, gamesFolder)}
	s, ok := readGamesState()
	if ok {
		r["chosen"] = s
		r["open"] = isMountpoint(gamesMount)
	}
	vs, err := listVolumes()
	if err != nil {
		return fail(err, "")
	}
	var can []reply
	for _, v := range vs {
		e := gamesCandidate(v)
		c := reply{"path": v.Path, "label": v.Label, "fstype": v.FSType, "size": v.Size, "model": v.Model, "usb": v.USB, "uuid": v.UUID}
		if e != nil {
			c["why"] = e.Error()
		}
		can = append(can, c)
	}
	r["disks"] = can
	return r
}

// gamesDiskSet is "games.diskSet" (owner only): dev "" turns it off.
func gamesDiskSet(dev string) reply {
	args := []string{"-n", "/opt/oros/oros-system", "mount", "--games-off"}
	if dev != "" {
		args = []string{"-n", "/opt/oros/oros-system", "mount", "--games-set", dev}
	}
	if out, err := run("sudo", args...); err != nil {
		return fail(err, out)
	}
	if dev != "" {
		if err := gamesOverride(); err != nil {
			logf("games: flatpak override: %v", err)
		}
	}
	return gamesDiskStatus()
}

// gamesOverride lets every Flatpak program (the center's game stores)
// write to the game library: flatpak's own override, which adds the place
// to [Context] filesystems and keeps other overrides.
func gamesOverride() error {
	if _, err := os.Stat(gamesStateFile); err != nil {
		return nil
	}
	out, err := run(flatpakBin, "--user", "override", "--filesystem="+gamesMount)
	return cmdErrIf("flatpak override", out, err)
}
