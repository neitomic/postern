package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testDigest(id, secret string) [32]byte {
	return sha256.Sum256([]byte("postern-join-v1:" + id + ":" + secret))
}

func testToken(id string, expiresAt int64, bound *string) *Token {
	sum := testDigest(id, "secret")
	return &Token{
		ID:         id,
		Kind:       KindJoin,
		SecretHash: hex.EncodeToString(sum[:]),
		ExpiresAt:  expiresAt,
		CreatedAt:  1,
		BoundName:  bound,
	}
}

func TestListAndRevokeTokens(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)

	a := testToken("aaaaaaaaaaaaaaaa", 100, strPtr("macbook"))
	a.CreatedAt = 10
	n := "first"
	a.Note = &n
	if err := st.InsertToken(a); err != nil {
		t.Fatal(err)
	}
	b := testToken("bbbbbbbbbbbbbbbb", 200, nil)
	b.CreatedAt = 20
	n2 := "second"
	b.Note = &n2
	if err := st.InsertToken(b); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListTokens()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("order = %q, %q", list[0].ID, list[1].ID)
	}
	if list[0].Note == nil || *list[0].Note != "second" {
		t.Fatalf("note = %v", list[0].Note)
	}

	if err := st.RevokeToken(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TokenByID(a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TokenByID after revoke: %v, want %v", err, sql.ErrNoRows)
	}
	if err := st.RevokeToken(a.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("second revoke: %v, want %v", err, ErrTokenNotFound)
	}

	list, err = st.ListTokens()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("after revoke list = %+v", list)
	}
}

func TestRevokeMissing(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	if err := st.RevokeToken("missingidmissing"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("RevokeToken missing: %v", err)
	}
}

func TestConsumeTokenStore(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	row := testToken("cccccccccccccccc", 100, strPtr("pi"))
	if err := st.InsertToken(row); err != nil {
		t.Fatal(err)
	}
	digest := testDigest(row.ID, "secret")

	got, err := st.ConsumeToken(row.ID, digest, "pi", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedAt == nil {
		t.Fatal("used_at not set")
	}

	_, err = st.ConsumeToken(row.ID, digest, "pi", 10)
	if !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("replay: %v, want %v", err, ErrTokenUsed)
	}
}

func TestConsumeTokenWrongDigest(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	row := testToken("dddddddddddddddd", 100, nil)
	if err := st.InsertToken(row); err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	_, err := st.ConsumeToken(row.ID, digest, "", 10)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("zero digest: %v, want %v", err, ErrInvalidToken)
	}
	got, err := st.TokenByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedAt != nil {
		t.Fatal("wrong digest marked used")
	}
}

func TestConsumeTokenNotFound(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	var digest [32]byte
	_, err := st.ConsumeToken("abcdefghijklmnop", digest, "", 1)
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("missing: %v, want %v", err, ErrTokenNotFound)
	}
}

func TestConsumeTokenExpiredAndBoundName(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)

	tok := testToken("eeeeeeeeeeeeeeee", 50, strPtr("macbook"))
	if err := st.InsertToken(tok); err != nil {
		t.Fatal(err)
	}
	sum := testDigest(tok.ID, "secret")

	_, err := st.ConsumeToken(tok.ID, sum, "macbook", 50)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired: %v, want %v", err, ErrTokenExpired)
	}

	tok2 := testToken("ffffffffffffffff", 100, strPtr("macbook"))
	if err := st.InsertToken(tok2); err != nil {
		t.Fatal(err)
	}
	sum2 := testDigest(tok2.ID, "secret")
	_, err = st.ConsumeToken(tok2.ID, sum2, "nuc", 10)
	if !errors.Is(err, ErrBoundName) {
		t.Fatalf("bound: %v, want %v", err, ErrBoundName)
	}
	if _, err := st.ConsumeToken(tok2.ID, sum2, "MACBOOK", 10); err != nil {
		t.Fatal(err)
	}
}

func TestInsertTokenRejectsOtherKind(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	tok := &Token{
		ID:         "abcdefghijklmnop",
		Kind:       "client",
		SecretHash: strings.Repeat("ab", 32),
		ExpiresAt:  100,
		CreatedAt:  1,
	}
	if err := st.InsertToken(tok); err == nil {
		t.Fatal("expected kind check to reject client")
	}
}

func strPtr(s string) *string { return &s }
