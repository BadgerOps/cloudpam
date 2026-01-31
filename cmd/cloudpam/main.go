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

	ih "cloudpam/internal/http"
	"cloudpam/internal/observability"
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
	migrate := flag.String("migrate", "", "run migrations: 'up' to apply, 'status' to show status (sqlite builds only)")
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

	rateCfg := ih.DefaultRateLimitConfig()
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

	mux := http.NewServeMux()
	srv := ih.NewServer(mux, store, logger, metrics, nil) // nil audit logger uses memory implementation
	srv.RegisterRoutes()

	// Apply middleware stack (metrics, request ID, structured logging, rate limiting).
	// Order: metrics (outermost) -> requestID -> logging -> rateLimiting (innermost before handler)
	handler := ih.ApplyMiddlewares(
		mux,
		observability.MetricsMiddleware(metrics),
		ih.RequestIDMiddleware(),
		ih.LoggingMiddleware(logger.Slog()),
		ih.RateLimitMiddleware(rateCfg, logger.Slog()),
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

// runMigrationsCLI executes migration commands (sqlite builds only).
func runMigrationsCLI(logger observability.Logger, cmd string) {
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	switch cmd {
	case "up":
		// initialize store (runs migrations in sqlite build), then show status
		_ = selectStore(logger)
		fallthrough
	case "status":
		status := "migrations status not available in this build"
		if s := sqliteStatus(dsn); s != "" {
			status = s
		}
		logger.Info("migrations status", "status", status)
	default:
		logger.Warn("unknown migrate command", "command", cmd)
	}
}
