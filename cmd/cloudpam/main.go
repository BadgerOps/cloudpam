package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"

	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	awscollector "cloudpam/internal/discovery/aws"
	"cloudpam/internal/api"
	"cloudpam/internal/observability"
	"cloudpam/internal/planning"
	"cloudpam/internal/planning/llm"

	"github.com/google/uuid"
)

func main() {
	// Initialize structured logger from environment configuration
	logCfg := observability.ConfigFromEnv()
	logger := observability.NewLogger(logCfg)

	addr := envOr("ADDR", ":8080")
	if p := os.Getenv("PORT"); p != "" { // Heroku-style
		addr = ":" + p
	}
	flag.StringVar(&addr, "addr", addr, "listen address (host:port)")
	migrate := flag.String("migrate", "", "run migrations: 'up' to apply, 'status' to show status")
	flag.Parse()

	// Initialize Sentry if DSN is provided
	sentryDSN := os.Getenv("SENTRY_DSN")
	sentryEnabled := false
	if sentryDSN != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:              sentryDSN,
			Environment:      envOr("SENTRY_ENVIRONMENT", "production"),
			Release:          envOr("APP_VERSION", "dev"),
			TracesSampleRate: 1.0, // Capture 100% of transactions for performance monitoring
			AttachStacktrace: true,
		})
		if err != nil {
			logger.Warn("sentry initialization failed", "error", err)
		} else {
			logger.Info("sentry initialized",
				"environment", envOr("SENTRY_ENVIRONMENT", "production"),
				"release", envOr("APP_VERSION", "dev"),
			)
			sentryEnabled = true
		}
	}

	// Handle migrations CLI before starting server
	if *migrate != "" {
		runMigrationsCLI(logger, *migrate)
		return
	}

	// Select storage based on build tags and env (see store_*.go in this package).
	store := selectStore(logger)

	// Initialize metrics
	metricsCfg := observability.MetricsConfigFromEnv()
	var metrics *observability.Metrics
	if metricsCfg.Enabled {
		metrics = observability.NewMetrics(metricsCfg)
		logger.Info("metrics enabled",
			"namespace", metricsCfg.Namespace,
			"version", metricsCfg.Version,
		)
	} else {
		logger.Info("metrics disabled")
	}

	rateCfg := api.DefaultRateLimitConfig()
	if rpsVal := strings.TrimSpace(os.Getenv("RATE_LIMIT_RPS")); rpsVal != "" {
		if parsed, err := strconv.ParseFloat(rpsVal, 64); err != nil {
			logger.Warn("invalid RATE_LIMIT_RPS; disabling rate limiting", "value", rpsVal, "error", err)
			rateCfg.RequestsPerSecond = 0
		} else if parsed <= 0 {
			logger.Warn("non-positive RATE_LIMIT_RPS; disabling rate limiting", "value", parsed)
			rateCfg.RequestsPerSecond = 0
		} else {
			rateCfg.RequestsPerSecond = parsed
		}
	}
	if burstVal := strings.TrimSpace(os.Getenv("RATE_LIMIT_BURST")); burstVal != "" {
		if parsed, err := strconv.Atoi(burstVal); err != nil {
			logger.Warn("invalid RATE_LIMIT_BURST; using default", "value", burstVal, "error", err)
		} else if parsed <= 0 {
			logger.Warn("non-positive RATE_LIMIT_BURST; disabling rate limiting", "value", parsed)
			rateCfg.Burst = 0
		} else {
			rateCfg.Burst = parsed
		}
	}
	if !rateCfg.Enabled() {
		logger.Info("rate limiting disabled")
	} else {
		logger.Info("rate limiting configured",
			"requests_per_second", rateCfg.RequestsPerSecond,
			"burst", rateCfg.Burst,
		)
	}

	// Parse trusted proxies for X-Forwarded-For handling
	var proxyConfig *api.TrustedProxyConfig
	if proxiesEnv := os.Getenv("CLOUDPAM_TRUSTED_PROXIES"); proxiesEnv != "" {
		var err error
		proxyConfig, err = api.ParseTrustedProxies(proxiesEnv)
		if err != nil {
			logger.Error("invalid CLOUDPAM_TRUSTED_PROXIES", "error", err)
		} else {
			logger.Info("trusted proxies configured", "count", len(proxyConfig.CIDRs))
		}
	}

	mux := http.NewServeMux()
	auditLogger := selectAuditLogger(logger)
	keyStore := selectKeyStore(logger)
	userStore := selectUserStore(logger)
	sessionStore := selectSessionStore(logger)
	srv := api.NewServer(mux, store, logger, metrics, auditLogger)

	// Check if this is a fresh install (no users exist) for first-boot setup.
	srv.SetUserStore(userStore)
	existingUsers, _ := userStore.List(context.Background())
	if len(existingUsers) == 0 {
		srv.SetNeedsSetup(true)
		logger.Info("fresh install detected: setup wizard enabled at /setup")
	}

	// Bootstrap admin user from environment variables (idempotent).
	if adminUser := os.Getenv("CLOUDPAM_ADMIN_USERNAME"); adminUser != "" {
		adminPass := os.Getenv("CLOUDPAM_ADMIN_PASSWORD")
		if adminPass == "" {
			logger.Error("CLOUDPAM_ADMIN_USERNAME set but CLOUDPAM_ADMIN_PASSWORD is empty")
		} else {
			bootstrapAdmin(logger, userStore, adminUser, adminPass)
		}
	}

	// Initialize discovery subsystem
	discoveryStore := selectDiscoveryStore(logger, store)
	syncService := discovery.NewSyncService(discoveryStore)
	syncService.RegisterCollector(awscollector.New())
	discoverySrv := api.NewDiscoveryServer(srv, discoveryStore, syncService, keyStore)
	logger.Info("discovery subsystem initialized", "collectors", "aws")

	// Initialize analysis subsystem
	analysisService := planning.NewAnalysisService(store)
	analysisSrv := api.NewAnalysisServer(srv, analysisService)
	logger.Info("analysis subsystem initialized")

	// Initialize recommendation subsystem
	recStore := selectRecommendationStore(logger, store)
	recService := planning.NewRecommendationService(analysisService, recStore, store)
	recSrv := api.NewRecommendationServer(srv, recService, recStore)
	logger.Info("recommendation subsystem initialized")

	// Initialize AI planning subsystem
	llmCfg := llm.ConfigFromEnv()
	var llmProvider llm.Provider
	if llmCfg.APIKey != "" {
		llmProvider = llm.NewOpenAIProvider(llmCfg)
		logger.Info("ai planning enabled", "model", llmCfg.Model, "endpoint", llmCfg.Endpoint)
	} else {
		llmProvider = llm.NewOpenAIProvider(llmCfg) // unconfigured; Available() returns false
		logger.Info("ai planning disabled (set CLOUDPAM_LLM_API_KEY to enable)")
	}
	convStore := selectConversationStore(logger, store)
	aiService := planning.NewAIPlanningService(analysisService, convStore, store, llmProvider)
	aiSrv := api.NewAIPlanningServer(srv, aiService, convStore)
	logger.Info("ai planning subsystem initialized")

	// Auth is always enabled — register protected routes with RBAC.
	srv.RegisterProtectedRoutes(keyStore, sessionStore, userStore, logger.Slog())
	authSrv := api.NewAuthServerWithStores(srv, keyStore, sessionStore, userStore, auditLogger)
	authSrv.RegisterProtectedAuthRoutes(logger.Slog())
	userSrv := api.NewUserServer(srv, keyStore, userStore, sessionStore, auditLogger)
	loginRL := api.LoginRateLimitMiddleware(api.LoginRateLimitConfig{
		AttemptsPerMinute: 5,
		ProxyConfig:       proxyConfig,
	})
	userSrv.RegisterProtectedUserRoutes(logger.Slog(), api.WithLoginRateLimit(loginRL))
	dualMW := api.DualAuthMiddleware(keyStore, sessionStore, userStore, true, logger.Slog())
	discoverySrv.RegisterProtectedDiscoveryRoutes(dualMW, logger.Slog())
	analysisSrv.RegisterProtectedAnalysisRoutes(dualMW, logger.Slog())
	recSrv.RegisterProtectedRecommendationRoutes(dualMW, logger.Slog())
	aiSrv.RegisterProtectedAIPlanningRoutes(dualMW, logger.Slog())

	if len(existingUsers) == 0 {
		logger.Info("first-boot setup required", "hint", "visit the UI to create an admin account")
	} else {
		logger.Info("authentication enforced", "users", len(existingUsers))
	}

	// Background session cleanup every 15 minutes.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			n, err := sessionStore.Cleanup(context.Background())
			if err != nil {
				logger.Warn("session cleanup error", "error", err)
			} else if n > 0 {
				logger.Info("cleaned up expired sessions", "count", n)
			}
		}
	}()

	// Apply middleware stack (metrics, request ID, structured logging, rate limiting).
	// Order: metrics (outermost) -> requestID -> logging -> rateLimiting (innermost before handler)
	handler := api.ApplyMiddlewares(
		mux,
		observability.MetricsMiddleware(metrics),
		api.RequestIDMiddleware(),
		api.LoggingMiddleware(logger.Slog()),
		api.RateLimitMiddleware(rateCfg, logger.Slog()),
	)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("cloudpam listening", "addr", addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Wait for interrupt signal or server error
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	case sig := <-sigChan:
		logger.Info("received shutdown signal", "signal", sig)
	}

	// Graceful shutdown with 15-second timeout
	logger.Info("shutting down server", "timeout", "15s")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	} else {
		logger.Info("server stopped gracefully")
	}

	// Close database connection
	if err := store.Close(); err != nil {
		logger.Error("error closing store", "error", err)
	} else {
		logger.Info("database connection closed")
	}

	// Flush Sentry events
	if sentryEnabled {
		logger.Info("flushing sentry events", "deadline", "2s")
		sentry.Flush(2 * time.Second)
	}

	logger.Info("shutdown complete")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// bootstrapAdmin creates the initial admin user if it doesn't already exist.
func bootstrapAdmin(logger observability.Logger, userStore auth.UserStore, username, password string) {
	existing, _ := userStore.GetByUsername(context.Background(), username)
	if existing != nil {
		logger.Info("bootstrap admin already exists", "username", username)
		return
	}

	if err := auth.ValidatePassword(password, 0); err != nil {
		logger.Error("bootstrap admin password does not meet requirements", "error", err)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		logger.Error("failed to hash admin password", "error", err)
		return
	}

	now := time.Now().UTC()
	user := &auth.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        envOr("CLOUDPAM_ADMIN_EMAIL", username+"@localhost"),
		DisplayName:  username,
		Role:         auth.RoleAdmin,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := userStore.Create(context.Background(), user); err != nil {
		logger.Error("failed to create bootstrap admin", "error", err)
		return
	}
	logger.Info("bootstrap admin user created", "username", username)
}

// runMigrationsCLI executes migration commands.
func runMigrationsCLI(logger observability.Logger, cmd string) {
	switch cmd {
	case "up":
		// Initialize store (runs migrations automatically), then show status
		st := selectStore(logger)
		_ = st.Close()
		runMigrationsCLI(logger, "status")
	case "status":
		status := "migrations status not available in this build"
		// Try SQLite status first
		dsn := os.Getenv("SQLITE_DSN")
		if dsn == "" {
			dsn = "file:cloudpam.db?cache=shared&_fk=1"
		}
		if s := sqliteStatus(dsn); s != "" {
			status = s
		}
		// Try PostgreSQL status
		if s := postgresStatus(); s != "" {
			status = s
		}
		logger.Info("migrations status", "status", status)
	default:
		logger.Warn("unknown migrate command", "command", cmd)
	}
}
