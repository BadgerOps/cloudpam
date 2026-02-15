//go:build sqlite && postgres

package main

import (
	"os"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
	pgstore "cloudpam/internal/storage/postgres"
	sqlitestore "cloudpam/internal/storage/sqlite"
)

func usePostgres() bool {
	return os.Getenv("DATABASE_URL") != ""
}

func sqliteDSN() string {
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	return dsn
}

func databaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://cloudpam:cloudpam@localhost:5432/cloudpam?sslmode=disable"
	}
	return url
}

// selectStore picks PostgreSQL if DATABASE_URL is set, otherwise SQLite.
func selectStore(logger observability.Logger) storage.Store {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if usePostgres() {
		url := databaseURL()
		st, err := pgstore.New(url)
		if err != nil {
			logger.Error("postgres init failed; falling back to sqlite", "error", err)
		} else {
			logger.Info("using postgres store")
			return st
		}
	}
	dsn := sqliteDSN()
	st, err := sqlitestore.New(dsn)
	if err != nil {
		logger.Error("sqlite init failed; falling back to memory store", "error", err)
		return storage.NewMemoryStore()
	}
	logger.Info("using sqlite store", "dsn", dsn)
	return st
}

func selectAuditLogger(logger observability.Logger) audit.AuditLogger {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if usePostgres() {
		al, err := audit.NewPostgresAuditLogger(databaseURL())
		if err != nil {
			logger.Error("postgres audit logger init failed; falling back to sqlite", "error", err)
		} else {
			logger.Info("using postgres audit logger")
			return al
		}
	}
	al, err := audit.NewSQLiteAuditLogger(sqliteDSN())
	if err != nil {
		logger.Error("sqlite audit logger init failed; falling back to memory", "error", err)
		return audit.NewMemoryAuditLogger()
	}
	logger.Info("using sqlite audit logger")
	return al
}

func selectKeyStore(logger observability.Logger) auth.KeyStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if usePostgres() {
		ks, err := auth.NewPostgresKeyStore(databaseURL())
		if err != nil {
			logger.Error("postgres key store init failed; falling back to sqlite", "error", err)
		} else {
			logger.Info("using postgres key store")
			return ks
		}
	}
	ks, err := auth.NewSQLiteKeyStore(sqliteDSN())
	if err != nil {
		logger.Error("sqlite key store init failed; falling back to memory", "error", err)
		return auth.NewMemoryKeyStore()
	}
	logger.Info("using sqlite key store")
	return ks
}

func selectUserStore(logger observability.Logger) auth.UserStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if usePostgres() {
		us, err := auth.NewPostgresUserStore(databaseURL())
		if err != nil {
			logger.Error("postgres user store init failed; falling back to sqlite", "error", err)
		} else {
			logger.Info("using postgres user store")
			return us
		}
	}
	us, err := auth.NewSQLiteUserStore(sqliteDSN())
	if err != nil {
		logger.Error("sqlite user store init failed; falling back to memory", "error", err)
		return auth.NewMemoryUserStore()
	}
	logger.Info("using sqlite user store")
	return us
}

func selectSessionStore(logger observability.Logger) auth.SessionStore {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if usePostgres() {
		ss, err := auth.NewPostgresSessionStore(databaseURL())
		if err != nil {
			logger.Error("postgres session store init failed; falling back to sqlite", "error", err)
		} else {
			logger.Info("using postgres session store")
			return ss
		}
	}
	ss, err := auth.NewSQLiteSessionStore(sqliteDSN())
	if err != nil {
		logger.Error("sqlite session store init failed; falling back to memory", "error", err)
		return auth.NewMemorySessionStore()
	}
	logger.Info("using sqlite session store")
	return ss
}

func selectDiscoveryStore(logger observability.Logger, mainStore storage.Store) storage.DiscoveryStore {
	if ds, ok := mainStore.(storage.DiscoveryStore); ok {
		return ds
	}
	logger.Warn("main store does not implement DiscoveryStore; using in-memory fallback")
	return storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
}

func selectRecommendationStore(logger observability.Logger, mainStore storage.Store) storage.RecommendationStore {
	if rs, ok := mainStore.(storage.RecommendationStore); ok {
		return rs
	}
	logger.Warn("main store does not implement RecommendationStore; using in-memory fallback")
	return storage.NewMemoryRecommendationStore(storage.NewMemoryStore())
}

func sqliteStatus(dsn string) string {
	s, err := sqlitestore.Status(dsn)
	if err != nil {
		return ""
	}
	return s
}

func postgresStatus() string {
	s, err := pgstore.Status(databaseURL())
	if err != nil {
		return ""
	}
	return s
}
