package store

import (
	"database/sql"
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
	var t Token
	var usedAt sql.NullInt64
	var note, bound sql.NullString
	if err := row.Scan(&t.ID, &t.Kind, &t.SecretHash, &t.ExpiresAt, &usedAt, &t.CreatedAt, &note, &bound); err != nil {
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
