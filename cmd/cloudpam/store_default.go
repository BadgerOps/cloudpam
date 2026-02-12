//go:build !sqlite && !postgres

package main

import (
	"os"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// selectStore returns the default storage when built without the 'sqlite' or 'postgres' tag.
// If SQLITE_DSN or DATABASE_URL is set, we log a hint to rebuild with the appropriate tag.
func selectStore(logger observability.Logger) storage.Store {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if os.Getenv("SQLITE_DSN") != "" {
		logger.Warn("SQLITE_DSN set but binary not built with -tags sqlite; using in-memory store")
	}
	if os.Getenv("DATABASE_URL") != "" {
		logger.Warn("DATABASE_URL set but binary not built with -tags postgres; using in-memory store")
	}
	return storage.NewMemoryStore()
}

// selectAuditLogger returns an in-memory audit logger when built without 'sqlite' tag.
func selectAuditLogger(logger observability.Logger) audit.AuditLogger {
	return audit.NewMemoryAuditLogger()
}

// selectKeyStore returns an in-memory key store when built without 'sqlite' tag.
func selectKeyStore(logger observability.Logger) auth.KeyStore {
	return auth.NewMemoryKeyStore()
}

// sqliteStatus returns schema status string when not built with sqlite tag.
func sqliteStatus(dsn string) string { return "" }

// postgresStatus returns schema status string when not built with postgres tag.
func postgresStatus() string { return "" }
