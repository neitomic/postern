package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func migrate(db *sql.DB) error {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, file := range files {
		ver, err := parseVersion(path.Base(file))
		if err != nil {
			return err
		}
		if _, ok := applied[ver]; ok {
			continue
		}
		body, err := migrationFS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		if err := applyMigration(db, ver, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}
		applied[ver] = struct{}{}
	}
	return nil
}

func applyMigration(db *sql.DB, version int, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range splitStatements(body) {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().Unix(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func appliedVersions(db *sql.DB) (map[int]struct{}, error) {
	out := make(map[int]struct{})
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&name)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}

func parseVersion(filename string) (int, error) {
	i := 0
	for i < len(filename) && unicode.IsDigit(rune(filename[i])) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("migration %q: missing numeric prefix", filename)
	}
	v, err := strconv.Atoi(filename[:i])
	if err != nil {
		return 0, fmt.Errorf("migration %q: %w", filename, err)
	}
	return v, nil
}

func splitStatements(s string) []string {
	parts := strings.Split(s, ";")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
