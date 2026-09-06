package store

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

const dbFileMode = 0o640

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_fk=1&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = db.Close()
		}
	}()
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := os.Chmod(path, dbFileMode); err != nil {
		return nil, fmt.Errorf("chmod %o: %w", dbFileMode, err)
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	ok = true
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
