//go:build sqlite

package main

import (
	"log"
	"os"

	"cloudpam/internal/storage"
	sqlitestore "cloudpam/internal/storage/sqlite"
)

// selectStore returns a SQLite-backed store when built with the 'sqlite' tag.
// Configure with env var SQLITE_DSN (e.g., file:cloudpam.db?cache=shared&_fk=1)
func selectStore() storage.Store {
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	st, err := sqlitestore.New(dsn)
	if err != nil {
		log.Printf("sqlite init failed (%v), falling back to memory store", err)
		return storage.NewMemoryStore()
	}
	log.Printf("using sqlite store: %s", dsn)
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
