package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

const KindJoin = "join"

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token_expired")
	ErrTokenUsed     = errors.New("token_used")
	ErrBoundName     = errors.New("bound_name mismatch")
	ErrInvalidToken  = errors.New("invalid token")
)

type Token struct {
	ID         string
	Kind       string
	SecretHash string
	ExpiresAt  int64
	UsedAt     *int64
	CreatedAt  int64
	Note       *string
	BoundName  *string
}

func (s *Store) InsertToken(t *Token) error {
	_, err := s.db.Exec(
		`INSERT INTO tokens (id, kind, secret_hash, expires_at, used_at, created_at, note, bound_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Kind, t.SecretHash, t.ExpiresAt, t.UsedAt, t.CreatedAt, t.Note, t.BoundName,
	)
	return err
}

func (s *Store) TokenByID(id string) (*Token, error) {
	row := s.db.QueryRow(
		`SELECT id, kind, secret_hash, expires_at, used_at, created_at, note, bound_name
		 FROM tokens WHERE id = ?`,
		id,
	)
	return scanToken(row)
}

func (s *Store) ListTokens() ([]*Token, error) {
	rows, err := s.db.Query(
		`SELECT id, kind, secret_hash, expires_at, used_at, created_at, note, bound_name
		 FROM tokens ORDER BY created_at DESC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ExpireUnusedTokens(now int64) (int, error) {
	res, err := s.db.Exec(`DELETE FROM tokens WHERE used_at IS NULL AND expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *Store) RevokeToken(id string) error {
	res, err := s.db.Exec(`DELETE FROM tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

func (s *Store) ConsumeToken(id string, digest [32]byte, enrollName string, now int64) (*Token, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	t, err := ConsumeTokenTx(tx, id, digest, enrollName, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return t, nil
}

func ConsumeTokenTx(tx *sql.Tx, id string, digest [32]byte, enrollName string, now int64) (*Token, error) {
	row := tx.QueryRow(
		`SELECT id, kind, secret_hash, expires_at, used_at, created_at, note, bound_name
		 FROM tokens WHERE id = ?`,
		id,
	)
	t, err := scanToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}

	stored, err := hex.DecodeString(t.SecretHash)
	if err != nil || len(stored) != sha256.Size {
		return nil, ErrInvalidToken
	}
	if subtle.ConstantTimeCompare(digest[:], stored) != 1 {
		return nil, ErrInvalidToken
	}
	if now >= t.ExpiresAt {
		return nil, ErrTokenExpired
	}
	if t.UsedAt != nil {
		return nil, ErrTokenUsed
	}
	if t.BoundName != nil && *t.BoundName != "" && !strings.EqualFold(*t.BoundName, enrollName) {
		return nil, ErrBoundName
	}

	res, err := tx.Exec(`UPDATE tokens SET used_at = ? WHERE id = ? AND used_at IS NULL`, now, id)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrTokenUsed
	}
	used := now
	t.UsedAt = &used
	return t, nil
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func scanToken(sc tokenScanner) (*Token, error) {
	var t Token
	var usedAt sql.NullInt64
	var note, bound sql.NullString
	if err := sc.Scan(&t.ID, &t.Kind, &t.SecretHash, &t.ExpiresAt, &usedAt, &t.CreatedAt, &note, &bound); err != nil {
		return nil, err
	}
	if usedAt.Valid {
		v := usedAt.Int64
		t.UsedAt = &v
	}
	if note.Valid {
		v := note.String
		t.Note = &v
	}
	if bound.Valid {
		v := bound.String
		t.BoundName = &v
	}
	return &t, nil
}
