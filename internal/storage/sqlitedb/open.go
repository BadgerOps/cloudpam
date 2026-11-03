//go:build sqlite

// Package sqlitedb opens SQLite handles with the connection settings every
// CloudPAM store needs.
package sqlitedb

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // CGO-less SQLite driver
)

// busy_timeout and foreign_keys are per-connection state, so they travel in
// the DSN, which the driver applies to every connection it opens. An open-time
// PRAGMA statement would reach only the first pooled connection.
const perConnectionPragmas = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// Open returns a pool whose every connection waits up to five seconds on a
// locked database and enforces foreign keys, with the file in WAL mode.
func Open(dsn string) (*sql.DB, error) {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("sqlite", dsn+sep+perConnectionPragmas)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}
	return db, nil
}
