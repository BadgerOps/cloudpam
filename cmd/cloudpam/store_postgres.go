//go:build postgres && !sqlite

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
		logger.Error("postgres init failed", "error", err)
		os.Exit(1)
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
		logger.Error("postgres audit logger init failed", "error", err)
		os.Exit(1)
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
		logger.Error("postgres key store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using postgres key store")
	return ks
}

// selectUserStore returns a PostgreSQL-backed user store.
func selectUserStore(logger observability.Logger) auth.UserStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	us, err := auth.NewPostgresUserStore(url)
	if err != nil {
		logger.Error("postgres user store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using postgres user store")
	return us
}

// selectRoleStore returns a PostgreSQL-backed role store.
func selectRoleStore(logger observability.Logger, userStore auth.UserStore) auth.RoleStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	rs, err := auth.NewPostgresRoleStore(url, userStore)
	if err != nil {
		logger.Error("postgres role store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using postgres role store")
	return rs
}

// selectSessionStore returns a PostgreSQL-backed session store.
func selectSessionStore(logger observability.Logger) auth.SessionStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	url := databaseURL()
	ss, err := auth.NewPostgresSessionStore(url)
	if err != nil {
		logger.Error("postgres session store init failed", "error", err)
		os.Exit(1)
	}
	logger.Info("using postgres session store")
	return ss
}

// selectDiscoveryStore returns the PostgreSQL store, which implements DiscoveryStore.
func selectDiscoveryStore(_ observability.Logger, mainStore storage.Store) storage.DiscoveryStore {
	return mainStore.(*pgstore.Store)
}

// selectRecommendationStore returns the PostgreSQL store, which implements RecommendationStore.
func selectRecommendationStore(_ observability.Logger, mainStore storage.Store) storage.RecommendationStore {
	return mainStore.(*pgstore.Store)
}

// selectConversationStore returns the PostgreSQL store, which implements ConversationStore.
func selectConversationStore(_ observability.Logger, mainStore storage.Store) storage.ConversationStore {
	return mainStore.(*pgstore.Store)
}

// selectDriftStore returns the PostgreSQL store, which implements DriftStore.
func selectDriftStore(_ observability.Logger, mainStore storage.Store) storage.DriftStore {
	return mainStore.(*pgstore.Store)
}

// selectNetworkStore returns the PostgreSQL store, which implements NetworkStore.
func selectNetworkStore(_ observability.Logger, mainStore storage.Store) storage.NetworkStore {
	return mainStore.(*pgstore.Store)
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
