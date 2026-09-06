package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "postern.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func TestOpenChmod0640(t *testing.T) {
	t.Parallel()
	_, path := openTemp(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != dbFileMode {
		t.Fatalf("db mode = %o, want %o", perm, dbFileMode)
	}
}

func TestOpenIdempotentAndWAL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "postern.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var mode string
	if err := st.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := st.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}
	var timeout int
	if err := st.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", timeout)
	}
}

func TestMigrationApplies(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)

	var version int
	if err := st.db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if version != 1 {
		t.Fatalf("schema_migrations.version = %d, want 1", version)
	}

	for _, table := range []string{"hosts", "tokens", "schema_migrations"} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}

	var clients string
	err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'clients'`).Scan(&clients)
	if err != sql.ErrNoRows {
		t.Fatalf("clients table should not exist, got name=%q err=%v", clients, err)
	}

	for _, idx := range []string{"idx_hosts_name", "idx_hosts_port", "idx_hosts_fingerprint"} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&name)
		if err != nil {
			t.Fatalf("index %s: %v", idx, err)
		}
	}
}

func TestUniqueConstraints(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)

	base := Host{
		Name:           "macbook",
		LoginUser:      "neo",
		Port:           2223,
		KeyFingerprint: "SHA256:aaaa",
		Pubkey:         "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIaaaa",
		TagsJSON:       "[]",
		CreatedAt:      1,
		UpdatedAt:      1,
	}
	h := base
	if err := st.InsertHost(&h); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if h.ID == 0 {
		t.Fatal("expected assigned id")
	}

	dupName := base
	dupName.Name = "macbook"
	dupName.Port = 2224
	dupName.KeyFingerprint = "SHA256:bbbb"
	if err := st.InsertHost(&dupName); err == nil {
		t.Fatal("expected unique name constraint")
	}

	dupCase := base
	dupCase.Name = "MacBook"
	dupCase.Port = 2225
	dupCase.KeyFingerprint = "SHA256:cccc"
	if err := st.InsertHost(&dupCase); err == nil {
		t.Fatal("expected unique name constraint (NOCASE)")
	}

	dupPort := base
	dupPort.Name = "nuc"
	dupPort.Port = 2223
	dupPort.KeyFingerprint = "SHA256:dddd"
	if err := st.InsertHost(&dupPort); err == nil {
		t.Fatal("expected unique port constraint")
	}

	dupFP := base
	dupFP.Name = "pi"
	dupFP.Port = 2201
	dupFP.KeyFingerprint = "SHA256:aaaa"
	if err := st.InsertHost(&dupFP); err == nil {
		t.Fatal("expected unique fingerprint constraint")
	}

	ok := base
	ok.Name = "nuc"
	ok.Port = 2201
	ok.KeyFingerprint = "SHA256:eeee"
	if err := st.InsertHost(&ok); err != nil {
		t.Fatalf("distinct host: %v", err)
	}

	got, err := st.HostByName("MACBOOK")
	if err != nil {
		t.Fatalf("HostByName: %v", err)
	}
	if got.Name != "macbook" || got.Port != 2223 {
		t.Fatalf("HostByName = %+v", got)
	}

	if _, err := st.HostByName("missing"); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("HostByName missing: %v, want %v", err, ErrHostNotFound)
	}

	list, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("ListHosts len = %d, want 2", len(list))
	}
	if list[0].Name != "macbook" || list[1].Name != "nuc" {
		t.Fatalf("ListHosts order = %q, %q", list[0].Name, list[1].Name)
	}
}

func TestListHostsEmpty(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	list, err := st.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if list == nil || len(list) != 0 {
		t.Fatalf("ListHosts empty = %#v", list)
	}
}

func TestInsertToken(t *testing.T) {
	t.Parallel()
	st, _ := openTemp(t)
	tok := &Token{
		ID:         "abcdefghijklmnop",
		Kind:       "join",
		SecretHash: strings.Repeat("ab", 32),
		ExpiresAt:  100,
		CreatedAt:  1,
	}
	if err := st.InsertToken(tok); err != nil {
		t.Fatal(err)
	}
	got, err := st.TokenByID(tok.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "join" || got.SecretHash != tok.SecretHash {
		t.Fatalf("TokenByID = %+v", got)
	}
	if err := st.InsertToken(tok); err == nil {
		t.Fatal("expected unique token id")
	}
}
