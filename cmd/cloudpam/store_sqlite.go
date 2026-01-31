//go:build sqlite

package main

import (
	"os"

	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
	sqlitestore "cloudpam/internal/storage/sqlite"
)

// selectStore returns a SQLite-backed store when built with the 'sqlite' tag.
// Configure with env var SQLITE_DSN (e.g., file:cloudpam.db?cache=shared&_fk=1)
func selectStore(logger observability.Logger) storage.Store {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	st, err := sqlitestore.New(dsn)
	if err != nil {
		logger.Error("sqlite init failed; falling back to memory store", "error", err)
		return storage.NewMemoryStore()
	}
	logger.Info("using sqlite store", "dsn", dsn)
	return st
}

// sqliteStatus returns migration status when built with sqlite tag.
func sqliteStatus(dsn string) string {
	s, err := sqlitestore.Status(dsn)
	if err != nil {
		return ""
	}
	return s
}
