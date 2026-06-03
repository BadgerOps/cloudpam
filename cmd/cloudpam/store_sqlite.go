//go:build sqlite && !postgres

package main

import (
	"os"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
	sqlitestore "cloudpam/internal/storage/sqlite"
)

func sqliteDSN() string {
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	return dsn
}

// selectStore returns a SQLite-backed store when built with the 'sqlite' tag.
// Configure with env var SQLITE_DSN (e.g., file:cloudpam.db?cache=shared&_fk=1)
func selectStore(logger observability.Logger) storage.Store {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	st, err := sqlitestore.New(dsn)
	if err != nil {
		logger.Error("sqlite init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite store", "dsn", dsn)
	return st
}

// selectAuditLogger returns a SQLite-backed audit logger when built with 'sqlite' tag.
func selectAuditLogger(logger observability.Logger) audit.AuditLogger {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	al, err := audit.NewSQLiteAuditLogger(dsn)
	if err != nil {
		logger.Error("sqlite audit logger init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite audit logger")
	return al
}

// selectKeyStore returns a SQLite-backed key store when built with 'sqlite' tag.
func selectKeyStore(logger observability.Logger) auth.KeyStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	ks, err := auth.NewSQLiteKeyStore(dsn)
	if err != nil {
		logger.Error("sqlite key store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite key store")
	return ks
}

// selectUserStore returns a SQLite-backed user store.
func selectUserStore(logger observability.Logger) auth.UserStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	us, err := auth.NewSQLiteUserStore(dsn)
	if err != nil {
		logger.Error("sqlite user store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite user store")
	return us
}

// selectRoleStore returns a SQLite-backed role store.
func selectRoleStore(logger observability.Logger, userStore auth.UserStore) auth.RoleStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	rs, err := auth.NewSQLiteRoleStore(dsn, userStore)
	if err != nil {
		logger.Error("sqlite role store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite role store")
	return rs
}

// selectSessionStore returns a SQLite-backed session store.
func selectSessionStore(logger observability.Logger) auth.SessionStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	dsn := sqliteDSN()
	ss, err := auth.NewSQLiteSessionStore(dsn)
	if err != nil {
		logger.Error("sqlite session store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using sqlite session store")
	return ss
}

// selectDiscoveryStore returns a SQLite-backed discovery store.
// The SQLite Store already implements storage.DiscoveryStore.
func selectDiscoveryStore(logger observability.Logger, mainStore storage.Store) storage.DiscoveryStore {
	if ds, ok := mainStore.(storage.DiscoveryStore); ok {
		return ds
	}
	// Fallback: use in-memory
	logger.Warn("main store does not implement DiscoveryStore; using in-memory fallback")
	return storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
}

// selectRecommendationStore returns a SQLite-backed recommendation store if the main store supports it.
func selectRecommendationStore(logger observability.Logger, mainStore storage.Store) storage.RecommendationStore {
	if rs, ok := mainStore.(storage.RecommendationStore); ok {
		return rs
	}
	logger.Warn("main store does not implement RecommendationStore; using in-memory fallback")
	return storage.NewMemoryRecommendationStore(storage.NewMemoryStore())
}

// selectConversationStore returns a SQLite-backed conversation store if the main store supports it.
func selectConversationStore(logger observability.Logger, mainStore storage.Store) storage.ConversationStore {
	if cs, ok := mainStore.(storage.ConversationStore); ok {
		return cs
	}
	logger.Warn("main store does not implement ConversationStore; using in-memory fallback")
	return storage.NewMemoryConversationStore(storage.NewMemoryStore())
}

// selectDriftStore returns a SQLite-backed drift store if the main store supports it.
func selectDriftStore(logger observability.Logger, mainStore storage.Store) storage.DriftStore {
	if ds, ok := mainStore.(storage.DriftStore); ok {
		return ds
	}
	logger.Warn("main store does not implement DriftStore; using in-memory fallback")
	return storage.NewMemoryDriftStore(storage.NewMemoryStore())
}

func selectNetworkStore(logger observability.Logger, mainStore storage.Store) storage.NetworkStore {
	if ns, ok := mainStore.(storage.NetworkStore); ok {
		return ns
	}
	logger.Warn("main store does not implement NetworkStore; using in-memory fallback")
	return storage.NewMemoryNetworkStore(storage.NewMemoryStore())
}

// sqliteStatus returns migration status when built with sqlite tag.
func sqliteStatus(dsn string) string {
	s, err := sqlitestore.Status(dsn)
	if err != nil {
		return ""
	}
	return s
}

// postgresStatus returns schema status string when not built with postgres tag.
func postgresStatus() string { return "" }
