package store

import (
	"database/sql"
	"errors"
	"strings"
)

var ErrHostNotFound = errors.New("no_such_host")

type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

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
	return InsertHostTx(s.db, h)
}

func InsertHostTx(q querier, h *Host) error {
	tags := h.TagsJSON
	if tags == "" {
		tags = "[]"
	}
	disabled := 0
	if h.Disabled {
		disabled = 1
	}
	res, err := q.Exec(
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
	return HostByNameTx(s.db, name)
}

func HostByNameTx(q querier, name string) (*Host, error) {
	row := q.QueryRow(
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
	return ListHostsTx(s.db)
}

func ListHostsTx(q querier) ([]*Host, error) {
	rows, err := q.Query(
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

func (s *Store) UpdateHostEnroll(name, loginUser, tagsJSON, pubkey string, updatedAt int64) error {
	return UpdateHostEnrollTx(s.db, name, loginUser, tagsJSON, pubkey, updatedAt)
}

func UpdateHostEnrollTx(q querier, name, loginUser, tagsJSON, pubkey string, updatedAt int64) error {
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	res, err := q.Exec(
		`UPDATE hosts SET login_user = ?, tags_json = ?, pubkey = ?, updated_at = ? WHERE name = ?`,
		loginUser, tagsJSON, pubkey, updatedAt, name,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrHostNotFound
	}
	return nil
}

func (s *Store) DeleteHostByName(name string) error {
	return DeleteHostByNameTx(s.db, name)
}

func DeleteHostByNameTx(q querier, name string) error {
	res, err := q.Exec(`DELETE FROM hosts WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrHostNotFound
	}
	return nil
}

// UniqueColumn reports the UNIQUE constraint column (e.g. "hosts.port") if err is a unique violation.
func UniqueColumn(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	const prefix = "UNIQUE constraint failed: "
	s := err.Error()
	i := strings.Index(s, prefix)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(prefix):]
	if j := strings.IndexAny(rest, " \t("); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
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
