package main

// Users on one stick (orOS Portable launcher, users.go there; orOS Bible
// "Users on one device"). The launcher keeps users.json and current.json
// in ~/oros/data; this host only reads them, to open the Internet window
// in the signed-in user's own profile and to keep the system update and
// the install to the owner.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

var userIDRe = regexp.MustCompile(`^u[0-9a-f]{8}$`)

type stickUser struct {
	ID    string `json:"id"`
	Owner bool   `json:"owner"`
	Dir   string `json:"dir"`
}

// signedInUser returns the signed-in user's folder (profiles, web) and
// whether that user is the owner. With one user, or nobody signed in, it
// is the data folder and the owner.
func signedInUser(home string) (dir string, owner bool) {
	data := filepath.Join(home, "oros", "data")
	var l struct {
		Users []stickUser `json:"users"`
		Guest bool        `json:"guest"`
	}
	if b, err := os.ReadFile(filepath.Join(data, "users.json")); err != nil || json.Unmarshal(b, &l) != nil {
		return data, true
	}
	if !(len(l.Users) >= 2 || (l.Guest && len(l.Users) >= 1)) {
		return data, true
	}
	var cur struct {
		ID string `json:"id"`
	}
	if b, err := os.ReadFile(filepath.Join(data, "users", "current.json")); err != nil || json.Unmarshal(b, &cur) != nil {
		return data, false
	}
	if cur.ID == "guest" && l.Guest {
		return filepath.Join(data, "users", "guest"), false
	}
	for _, u := range l.Users {
		if u.ID != cur.ID || !userIDRe.MatchString(u.ID) {
			continue
		}
		switch u.Dir {
		case "":
			return data, u.Owner
		case "users/" + u.ID:
			return filepath.Join(data, "users", u.ID), u.Owner
		}
	}
	return data, false
}
