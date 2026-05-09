package state

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreOpenerUsesEnvPathAndMigrates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	t.Setenv(EnvStateDB, dbPath)

	db, closeDB, err := Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer closeDB()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	assertTableExists(t, db, "events")
	assertTableExists(t, db, "agent_inbox")
	assertTableExists(t, db, "global_feed")
	assertTableExists(t, db, "cursors")
	assertTableExists(t, db, "memory_sync")
	assertTableExists(t, db, "summaries")
	assertTableExists(t, db, "schema_meta")
	assertSchemaVersion(t, db, "2")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("state db was not created: %v", err)
	}
}

func TestStoreOpenerDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvStateDB, "")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error: %v", err)
	}
	want := filepath.Join(home, ".aitask", "state.db")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func assertTableExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
	if err != nil {
		t.Fatalf("table %s missing: %v", name, err)
	}
}

func assertSchemaVersion(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT value FROM schema_meta WHERE key='schema_version'`).Scan(&got)
	if err != nil {
		t.Fatalf("schema_version missing: %v", err)
	}
	if got != want {
		t.Fatalf("schema_version = %q, want %q", got, want)
	}
}
