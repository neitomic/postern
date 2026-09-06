package auth

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

var (
	ErrInvalidPubkey = errors.New("invalid_pubkey")
	ErrPubkeyOptions = errors.New("pubkey options not allowed")
	ErrMultipleKeys  = errors.New("multiple keys")
	ErrNotEd25519    = errors.New("not ed25519")
)

// ParseEnrollPubkey parses a single ssh-ed25519 authorized_keys line.
// NULs, CR/LF, options, extra keys, and non-ed25519 types are rejected
// before anything is stored. The comment is discarded.
func ParseEnrollPubkey(raw []byte) (ssh.PublicKey, error) {
	if bytes.IndexByte(raw, 0) >= 0 || bytes.ContainsAny(raw, "\n\r") {
		return nil, ErrInvalidPubkey
	}
	key, _, opts, rest, err := ssh.ParseAuthorizedKey(bytes.TrimSpace(raw))
	if err != nil {
		return nil, ErrInvalidPubkey
	}
	if len(opts) != 0 {
		return nil, ErrPubkeyOptions
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrMultipleKeys
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		return nil, ErrNotEd25519
	}
	return key, nil
}

// MarshalStoredKey is type + payload only (comment stripped, no trailing newline).
func MarshalStoredKey(key ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}
