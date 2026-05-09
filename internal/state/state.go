package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const EnvStateDB = "AITASK_STATE_DB"

type StoreOpener struct {
	Path string
}

func NewStoreOpener() StoreOpener {
	return StoreOpener{}
}

func DefaultPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv(EnvStateDB)); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aitask", "state.db"), nil
}

func (o StoreOpener) ResolvePath() (string, error) {
	if value := strings.TrimSpace(o.Path); value != "" {
		return value, nil
	}
	return DefaultPath()
}

func (o StoreOpener) Open(ctx context.Context) (*sql.DB, func() error, error) {
	dbPath, err := o.ResolvePath()
	if err != nil {
		return nil, nil, err
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
			return nil, nil, err
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil, err
	}
	closeFn := db.Close
	if err := configure(ctx, db); err != nil {
		_ = closeFn()
		return nil, nil, err
	}
	return db, closeFn, nil
}

func Open(ctx context.Context) (*sql.DB, func() error, error) {
	return NewStoreOpener().Open(ctx)
}

func OpenPath(ctx context.Context, dbPath string) (*sql.DB, func() error, error) {
	return StoreOpener{Path: dbPath}.Open(ctx)
}

func Exists() (bool, string, error) {
	dbPath, err := DefaultPath()
	if err != nil {
		return false, "", err
	}
	_, err = os.Stat(dbPath)
	if err == nil {
		return true, dbPath, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, dbPath, nil
	}
	return false, dbPath, err
}

func configure(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=NORMAL"); err != nil {
		return err
	}
	return nil
}
