//go:build !sqlite

package main

import (
	"log"
	"os"

	"cloudpam/internal/storage"
)

// selectStore returns the default storage when built without the 'sqlite' tag.
// If SQLITE_DSN is set, we log a hint to rebuild with -tags sqlite.
func selectStore() storage.Store {
	if os.Getenv("SQLITE_DSN") != "" {
		log.Printf("SQLITE_DSN set, but binary not built with -tags sqlite; using in-memory store")
	}
	return storage.NewMemoryStore()
}

// sqliteStatus returns schema status string when not built with sqlite tag.
func sqliteStatus(dsn string) string { return "" }
