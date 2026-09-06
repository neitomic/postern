package store

import (
	"database/sql"
	"errors"
)

var ErrHostNotFound = errors.New("no_such_host")

type Host struct {
	ID             int64
	Name           string
	LoginUser      string
	Port           int
	KeyFingerprint string
	Pubkey         string
	TagsJSON       string
	LastSeen       *int64
	CreatedAt      int64
	UpdatedAt      int64
	Disabled       bool
}

func (s *Store) InsertHost(h *Host) error {
	tags := h.TagsJSON
	if tags == "" {
		tags = "[]"
	}
	disabled := 0
	if h.Disabled {
		disabled = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO hosts (name, login_user, port, key_fingerprint, pubkey, tags_json, last_seen, created_at, updated_at, disabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.LoginUser, h.Port, h.KeyFingerprint, h.Pubkey, tags, h.LastSeen, h.CreatedAt, h.UpdatedAt, disabled,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	h.ID = id
	h.TagsJSON = tags
	return nil
}

func (s *Store) HostByName(name string) (*Host, error) {
	row := s.db.QueryRow(
		`SELECT id, name, login_user, port, key_fingerprint, pubkey, tags_json, last_seen, created_at, updated_at, disabled
		 FROM hosts WHERE name = ?`,
		name,
	)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrHostNotFound
	}
	return h, err
}

func (s *Store) ListHosts() ([]*Host, error) {
	rows, err := s.db.Query(
		`SELECT id, name, login_user, port, key_fingerprint, pubkey, tags_json, last_seen, created_at, updated_at, disabled
		 FROM hosts ORDER BY name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Host, 0)
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func scanHost(sc tokenScanner) (*Host, error) {
	var h Host
	var lastSeen sql.NullInt64
	var disabled int
	if err := sc.Scan(
		&h.ID, &h.Name, &h.LoginUser, &h.Port, &h.KeyFingerprint, &h.Pubkey, &h.TagsJSON,
		&lastSeen, &h.CreatedAt, &h.UpdatedAt, &disabled,
	); err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		v := lastSeen.Int64
		h.LastSeen = &v
	}
	h.Disabled = disabled != 0
	return &h, nil
}
