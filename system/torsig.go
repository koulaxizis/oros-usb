package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
)

// Tor Browser's signature, checked in Go (ProtonMail go-crypto) as in
// orOS Portable (oros-portable launcher/tor.go). The image carries Tor
// Browser Developers' key (scripts/tor-key.sh); when that key cannot check
// a new release (an image older than a new or renewed subkey), the key is
// fetched again from torproject.org's Web Key Directory or keys.openpgp.org
// and used only with the fingerprint pinned here. A download that fails
// the check is deleted and never run.

// torFingerprint is Tor Browser Developers' signing key, as torproject.org
// publishes it (scripts/tor-key.sh pins the same); tests replace it.
var torFingerprint = "EF6E286DDA85EA2A4BA7DE684E2C6E8793298290"

const torMaxKey = 1 << 20

func torKeyURLs() []string {
	return []string{
		"https://openpgpkey.torproject.org/.well-known/openpgpkey/torproject.org/hu/kounek7zrdx745qydx6p59t9mqjpuhdf",
		"https://keys.openpgp.org/vks/v1/by-fingerprint/" + torFingerprint,
	}
}

// torKeys reads a keyring (binary or armored) and keeps only the key with
// the pinned fingerprint.
func torKeys(b []byte) (openpgp.EntityList, error) {
	l, err := openpgp.ReadKeyRing(bytes.NewReader(b))
	if err != nil {
		l, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	}
	if err != nil {
		return nil, fmt.Errorf("Tor Browser's signing key: %v", err)
	}
	for _, e := range l {
		if e.PrimaryKey != nil && strings.EqualFold(fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), torFingerprint) {
			return openpgp.EntityList{e}, nil
		}
	}
	return nil, errors.New("Tor Browser's signing key has an unexpected fingerprint")
}

func torCheck(key openpgp.EntityList, file, sig string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := os.Open(sig)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = openpgp.CheckArmoredDetachedSignature(key, f, s, nil)
	if err != nil && errors.Is(err, io.EOF) {
		// not armored: read it as a binary signature
		if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
			return err2
		}
		if _, err2 := s.Seek(0, io.SeekStart); err2 != nil {
			return err2
		}
		_, err = openpgp.CheckDetachedSignature(key, f, s, nil)
	}
	return err
}

// torKeyProblem: the key, not the file, is why the check failed.
func torKeyProblem(err error) bool {
	return errors.Is(err, pgperrors.ErrUnknownIssuer) || errors.Is(err, pgperrors.ErrKeyExpired) ||
		errors.Is(err, pgperrors.ErrSignatureExpired) || errors.Is(err, pgperrors.ErrKeyRevoked) ||
		strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "revoked")
}

// torVerifyGo checks the download with the image's key, and once more with
// a freshly fetched one (same fingerprint) when that key cannot.
func torVerifyGo(keyring, sig, file string) error {
	var first error
	if b, err := os.ReadFile(keyring); err == nil {
		key, err := torKeys(b)
		if err == nil {
			if err = torCheck(key, file, sig); err == nil {
				return nil
			}
			if !torKeyProblem(err) {
				return fmt.Errorf("signature check failed: %v", err)
			}
		}
		first = err
	}
	for _, u := range torKeyURLs() {
		var b bytes.Buffer
		if err := torGet(u, torMaxKey, &b, nil); err != nil {
			continue
		}
		key, err := torKeys(b.Bytes())
		if err != nil {
			continue
		}
		if err := torCheck(key, file, sig); err != nil {
			return fmt.Errorf("signature check failed: %v", err)
		}
		return nil
	}
	if first != nil {
		return fmt.Errorf("signature check failed: %v", first)
	}
	return errors.New("Tor Browser's signing key could not be found")
}
