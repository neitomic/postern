package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neitomic/postern/internal/names"
	"github.com/neitomic/postern/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "postern.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestIssueParseVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	plain, row, err := Issue(time.Minute, "macbook", "neo's mbp")
	if err != nil {
		t.Fatal(err)
	}
	if row.Kind != KindJoin {
		t.Fatalf("kind = %q, want %q", row.Kind, KindJoin)
	}
	if row.BoundName == nil || *row.BoundName != "macbook" {
		t.Fatalf("BoundName = %v", row.BoundName)
	}
	if row.Note == nil || *row.Note != "neo's mbp" {
		t.Fatalf("Note = %v", row.Note)
	}
	if len(row.SecretHash) != 64 {
		t.Fatalf("SecretHash len = %d, want 64", len(row.SecretHash))
	}
	if _, err := hex.DecodeString(row.SecretHash); err != nil {
		t.Fatalf("SecretHash hex: %v", err)
	}

	id, secret, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	if id != row.ID {
		t.Fatalf("Parse id = %q, want %q", id, row.ID)
	}
	if !strings.HasPrefix(plain, TokenPrefix+id+".") {
		t.Fatalf("plaintext = %q", plain)
	}
	sum := Hash(id, secret)
	if hex.EncodeToString(sum[:]) != row.SecretHash {
		t.Fatal("Hash(id, secret) does not match row.SecretHash")
	}
	if !Verify(plain, row.SecretHash) {
		t.Fatal("Verify failed on issued token")
	}

	// Parse accepts mixed case and surrounding space.
	id2, secret2, err := Parse("  " + strings.ToUpper(plain) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id || secret2 != secret {
		t.Fatalf("Parse mixed case: id=%q secret=%q", id2, secret2)
	}
}

func TestBase32Lengths(t *testing.T) {
	t.Parallel()
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	if got := len(strings.ToLower(enc.EncodeToString(make([]byte, 10)))); got != IDLen {
		t.Fatalf("id encoded len = %d, want %d", got, IDLen)
	}
	if got := len(strings.ToLower(enc.EncodeToString(make([]byte, 24)))); got != SecretLen {
		t.Fatalf("secret encoded len = %d, want %d", got, SecretLen)
	}

	plain, row, err := Issue(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	id, secret, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != IDLen {
		t.Fatalf("issued id len = %d, want %d", len(id), IDLen)
	}
	if len(secret) != SecretLen {
		t.Fatalf("issued secret len = %d, want %d", len(secret), SecretLen)
	}
	if len(row.ID) != IDLen {
		t.Fatalf("row.ID len = %d, want %d", len(row.ID), IDLen)
	}

	rawID, err := enc.DecodeString(strings.ToUpper(id))
	if err != nil || len(rawID) != 10 {
		t.Fatalf("id decode: len=%d err=%v", len(rawID), err)
	}
	rawSecret, err := enc.DecodeString(strings.ToUpper(secret))
	if err != nil || len(rawSecret) != 24 {
		t.Fatalf("secret decode: len=%d err=%v", len(rawSecret), err)
	}
}

func TestHashIsSHA256NotHMAC(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", IDLen)
	secret := strings.Repeat("a", SecretLen)
	got := Hash(id, secret)
	want := sha256.Sum256([]byte("postern-join-v1:" + id + ":" + secret))
	if got != want {
		t.Fatalf("Hash = %x, want %x", got, want)
	}
	if len(got) != 32 {
		t.Fatalf("digest len = %d, want 32", len(got))
	}
}

func TestVerifyConstantTimeCompareOnDigests(t *testing.T) {
	t.Parallel()
	plain, row, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := hex.DecodeString(row.SecretHash)
	if err != nil || len(sum) != 32 {
		t.Fatalf("decode hash: len=%d err=%v", len(sum), err)
	}
	var digest [32]byte
	copy(digest[:], sum)

	if subtle.ConstantTimeCompare(digest[:], digest[:]) != 1 {
		t.Fatal("identical 32-byte digests did not compare equal")
	}
	tampered := digest
	tampered[31] ^= 1
	if subtle.ConstantTimeCompare(digest[:], tampered[:]) == 1 {
		t.Fatal("distinct 32-byte digests compared equal")
	}
	if Verify(plain, hex.EncodeToString(tampered[:])) {
		t.Fatal("Verify succeeded on tampered digest")
	}

	// Uppercase hex of the same digest still verifies: compare is on decoded bytes.
	if !Verify(plain, strings.ToUpper(row.SecretHash)) {
		t.Fatal("Verify failed on uppercase hex of the same digest")
	}

	id, secret, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one base32 character in the secret (stay in alphabet).
	b := []byte(secret)
	if b[0] == 'a' {
		b[0] = 'b'
	} else {
		b[0] = 'a'
	}
	wrong := TokenPrefix + id + "." + string(b)
	if Verify(wrong, row.SecretHash) {
		t.Fatal("Verify succeeded on wrong secret")
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	t.Parallel()
	plain, _, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	id, secret, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"",
		"not-a-token",
		"psn_join_",
		"psn_join_onlyid",
		"psn_join_" + id, // missing secret
		"psn_join_." + secret,
		TokenPrefix + id + "." + secret + ".extra",
		TokenPrefix + id[:15] + "." + secret,       // short id
		TokenPrefix + id + "a." + secret,           // long id
		TokenPrefix + id + "." + secret[:38],       // short secret
		TokenPrefix + id + "." + secret + "a",      // long secret
		TokenPrefix + "0000000000000000." + secret, // invalid base32 (0)
		TokenPrefix + id + "." + strings.Repeat("1", SecretLen),
	}
	for _, c := range cases {
		if _, _, err := Parse(c); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Parse(%q) = %v, want %v", c, err, ErrInvalidToken)
		}
		if Verify(c, strings.Repeat("ab", 32)) {
			t.Errorf("Verify(%q) succeeded", c)
		}
	}
}

func TestIssueTTL(t *testing.T) {
	t.Parallel()
	_, row, err := Issue(0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := row.ExpiresAt - row.CreatedAt; got != int64(DefaultTTL.Seconds()) {
		t.Fatalf("default ttl seconds = %d, want %d", got, int64(DefaultTTL.Seconds()))
	}

	_, row, err = Issue(MaxTTL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := row.ExpiresAt - row.CreatedAt; got != int64(MaxTTL.Seconds()) {
		t.Fatalf("max ttl seconds = %d, want %d", got, int64(MaxTTL.Seconds()))
	}

	_, _, err = Issue(MaxTTL+time.Second, "", "")
	if !errors.Is(err, ErrTTL) {
		t.Fatalf("ttl > max: %v, want %v", err, ErrTTL)
	}
	_, _, err = Issue(-time.Second, "", "")
	if !errors.Is(err, ErrTTL) {
		t.Fatalf("negative ttl: %v, want %v", err, ErrTTL)
	}
}

func TestIssueBoundName(t *testing.T) {
	t.Parallel()
	_, row, err := Issue(time.Minute, "MacBook", "")
	if err != nil {
		t.Fatal(err)
	}
	if row.BoundName == nil || *row.BoundName != "macbook" {
		t.Fatalf("BoundName = %v, want macbook", row.BoundName)
	}

	_, _, err = Issue(time.Minute, "postern", "")
	if !errors.Is(err, names.ErrReserved) {
		t.Fatalf("reserved bound_name: %v, want %v", err, names.ErrReserved)
	}
	_, _, err = Issue(time.Minute, "Bad_Name", "")
	if !errors.Is(err, names.ErrInvalid) {
		t.Fatalf("invalid bound_name: %v, want %v", err, names.ErrInvalid)
	}

	_, row, err = Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if row.BoundName != nil {
		t.Fatalf("unbound BoundName = %v", row.BoundName)
	}
}

func TestConsumeRoundTrip(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "macbook", "note")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(row.CreatedAt, 0)
	got, err := Consume(st, plain, "MACBOOK", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedAt == nil || *got.UsedAt != now.Unix() {
		t.Fatalf("UsedAt = %v, want %d", got.UsedAt, now.Unix())
	}
	stored, err := st.TokenByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.UsedAt == nil || *stored.UsedAt != now.Unix() {
		t.Fatalf("persisted UsedAt = %v", stored.UsedAt)
	}
}

func TestConsumeExpire(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}

	_, err = Consume(st, plain, "", time.Unix(row.ExpiresAt, 0))
	if !errors.Is(err, store.ErrTokenExpired) {
		t.Fatalf("at expires_at: %v, want %v", err, store.ErrTokenExpired)
	}
	_, err = Consume(st, plain, "", time.Unix(row.ExpiresAt+1, 0))
	if !errors.Is(err, store.ErrTokenExpired) {
		t.Fatalf("after expiry: %v, want %v", err, store.ErrTokenExpired)
	}

	got, err := st.TokenByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedAt != nil {
		t.Fatalf("expired consume set used_at = %v", got.UsedAt)
	}

	if _, err := Consume(st, plain, "", time.Unix(row.ExpiresAt-1, 0)); err != nil {
		t.Fatalf("before expiry: %v", err)
	}
}

func TestConsumeReplay(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(row.CreatedAt, 0)
	if _, err := Consume(st, plain, "any", now); err != nil {
		t.Fatal(err)
	}
	_, err = Consume(st, plain, "any", now)
	if !errors.Is(err, store.ErrTokenUsed) {
		t.Fatalf("replay: %v, want %v", err, store.ErrTokenUsed)
	}
}

func TestConsumeBoundName(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "macbook", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(row.CreatedAt, 0)

	_, err = Consume(st, plain, "nuc", now)
	if !errors.Is(err, store.ErrBoundName) {
		t.Fatalf("mismatch: %v, want %v", err, store.ErrBoundName)
	}
	got, err := st.TokenByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedAt != nil {
		t.Fatal("bound_name mismatch marked token used")
	}

	if _, err := Consume(st, plain, "MacBook", now); err != nil {
		t.Fatalf("case-insensitive match: %v", err)
	}
}

func TestConsumeUnboundAcceptsAnyName(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}
	if _, err := Consume(st, plain, "whatever", time.Unix(row.CreatedAt, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestConsumeWrongSecret(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	plain, row, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertToken(&row); err != nil {
		t.Fatal(err)
	}
	id, secret, err := Parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte(secret)
	if b[0] == 'a' {
		b[0] = 'c'
	} else {
		b[0] = 'a'
	}
	_, err = Consume(st, TokenPrefix+id+"."+string(b), "", time.Unix(row.CreatedAt, 0))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong secret: %v, want %v", err, ErrInvalidToken)
	}
	if !errors.Is(err, store.ErrInvalidToken) {
		t.Fatalf("wrong secret not store.ErrInvalidToken: %v", err)
	}

	_, err = Consume(st, "not-a-token", "", time.Unix(row.CreatedAt, 0))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("malformed: %v, want %v", err, ErrInvalidToken)
	}
}

func TestIssueDistinctIDs(t *testing.T) {
	t.Parallel()
	_, a, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := Issue(time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.SecretHash == b.SecretHash {
		t.Fatal("expected distinct tokens")
	}
}
