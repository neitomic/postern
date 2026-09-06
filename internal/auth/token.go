package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

const (
	KindJoin     = store.KindJoin
	TokenPrefix  = "psn_join_"
	DefaultTTL   = 15 * time.Minute
	MaxTTL       = 24 * time.Hour
	IDLen        = 16
	SecretLen    = 39
	hashDomain   = "postern-join-v1:"
	idRawLen     = 10
	secretRawLen = 24 // 192 bits; unpadded base32 length is 39 (192/5 = 38.4)
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTTL          = errors.New("invalid ttl")

	b32 = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Issue returns plaintext psn_join_<id>.<secret> and a Token row (hash only).
// ttl of 0 uses DefaultTTL; values above MaxTTL are rejected.
func Issue(ttl time.Duration, boundName, note string) (plaintext string, row store.Token, err error) {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if ttl < 0 || ttl > MaxTTL {
		return "", store.Token{}, ErrTTL
	}
	if boundName != "" {
		boundName = strings.ToLower(boundName)
		if err := names.Valid(boundName); err != nil {
			return "", store.Token{}, err
		}
	}

	idRaw := make([]byte, idRawLen)
	if _, err := rand.Read(idRaw); err != nil {
		return "", store.Token{}, err
	}
	secretRaw := make([]byte, secretRawLen)
	if _, err := rand.Read(secretRaw); err != nil {
		return "", store.Token{}, err
	}

	id := encode(idRaw)
	secret := encode(secretRaw)
	sum := Hash(id, secret)
	now := time.Now()
	row = store.Token{
		ID:         id,
		Kind:       KindJoin,
		SecretHash: hex.EncodeToString(sum[:]),
		ExpiresAt:  now.Add(ttl).Unix(),
		CreatedAt:  now.Unix(),
	}
	if note != "" {
		n := note
		row.Note = &n
	}
	if boundName != "" {
		b := boundName
		row.BoundName = &b
	}
	return TokenPrefix + id + "." + secret, row, nil
}

func Parse(plaintext string) (id, secret string, err error) {
	p := strings.ToLower(strings.TrimSpace(plaintext))
	if !strings.HasPrefix(p, TokenPrefix) {
		return "", "", ErrInvalidToken
	}
	rest := p[len(TokenPrefix):]
	id, secret, ok := strings.Cut(rest, ".")
	if !ok || id == "" || secret == "" || strings.Contains(secret, ".") {
		return "", "", ErrInvalidToken
	}
	if len(id) != IDLen || len(secret) != SecretLen {
		return "", "", ErrInvalidToken
	}
	if _, err := decode(id, idRawLen); err != nil {
		return "", "", ErrInvalidToken
	}
	if _, err := decode(secret, secretRawLen); err != nil {
		return "", "", ErrInvalidToken
	}
	return id, secret, nil
}

// Hash is SHA-256 of "postern-join-v1:" + id + ":" + secret, not HMAC.
func Hash(id, secret string) [32]byte {
	return sha256.Sum256([]byte(hashDomain + id + ":" + secret))
}

// Verify compares 32-byte digests with ConstantTimeCompare, not hex strings.
func Verify(plaintext, storedHash string) bool {
	id, secret, err := Parse(plaintext)
	if err != nil {
		return false
	}
	stored, err := hex.DecodeString(storedHash)
	if err != nil || len(stored) != sha256.Size {
		return false
	}
	sum := Hash(id, secret)
	return subtle.ConstantTimeCompare(sum[:], stored) == 1
}

func Consume(st *store.Store, plaintext, enrollName string, now time.Time) (*store.Token, error) {
	id, secret, err := Parse(plaintext)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	return st.ConsumeToken(id, Hash(id, secret), enrollName, now.Unix())
}

func encode(raw []byte) string {
	return strings.ToLower(b32.EncodeToString(raw))
}

func decode(s string, wantBytes int) ([]byte, error) {
	raw, err := b32.DecodeString(strings.ToUpper(s))
	if err != nil {
		return nil, err
	}
	if len(raw) != wantBytes {
		return nil, ErrInvalidToken
	}
	return raw, nil
}
