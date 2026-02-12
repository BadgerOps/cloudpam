//go:build postgres

package main

import (
	"os"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
	pgstore "cloudpam/internal/storage/postgres"
)

func databaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://cloudpam:cloudpam@localhost:5432/cloudpam?sslmode=disable"
	}
	return url
}

// selectStore returns a PostgreSQL-backed store when built with the 'postgres' tag.
// Configure with env var DATABASE_URL.
func selectStore(logger observability.Logger) storage.Store {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	st, err := pgstore.New(url)
	if err != nil {
		logger.Error("postgres init failed; falling back to memory store", "error", err)
		return storage.NewMemoryStore()
	}
	logger.Info("using postgres store")
	return st
}

// selectAuditLogger returns a PostgreSQL-backed audit logger when built with 'postgres' tag.
func selectAuditLogger(logger observability.Logger) audit.AuditLogger {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	al, err := audit.NewPostgresAuditLogger(url)
	if err != nil {
		logger.Error("postgres audit logger init failed; falling back to memory", "error", err)
		return audit.NewMemoryAuditLogger()
	}
	logger.Info("using postgres audit logger")
	return al
}

// selectKeyStore returns a PostgreSQL-backed key store when built with 'postgres' tag.
func selectKeyStore(logger observability.Logger) auth.KeyStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	ks, err := auth.NewPostgresKeyStore(url)
	if err != nil {
		logger.Error("postgres key store init failed; falling back to memory", "error", err)
		return auth.NewMemoryKeyStore()
	}
	logger.Info("using postgres key store")
	return ks
}

// sqliteStatus is a no-op for postgres builds.
func sqliteStatus(_ string) string { return "" }

// postgresStatus returns migration status for postgres builds.
func postgresStatus() string {
	s, err := pgstore.Status(databaseURL())
	if err != nil {
		return ""
	}
	return s
}
