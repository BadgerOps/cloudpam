package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"

	ih "cloudpam/internal/http"
)

func main() {
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
			log.Printf("sentry initialization failed: %v", err)
		} else {
			log.Printf("sentry initialized (env=%s, release=%s)", envOr("SENTRY_ENVIRONMENT", "production"), envOr("APP_VERSION", "dev"))
			sentryEnabled = true
		}
	}

	// Handle migrations CLI before starting server
	if *migrate != "" {
		runMigrationsCLI(*migrate)
		return
	}

	// Select storage based on build tags and env (see store_*.go in this package).
	store := selectStore()
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("error closing store: %v", err)
		}
	}()

	mux := http.NewServeMux()
	srv := ih.NewServer(mux, store)
	srv.RegisterRoutes()

	// Wrap with simple logging middleware
	handler := ih.LoggingMiddleware(mux)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("cloudpam listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
		if sentryEnabled {
			sentry.Flush(2 * time.Second)
		}
		os.Exit(1)
	}

	// Graceful shutdown path (not usually reached in ListenAndServe example)
	if sentryEnabled {
		sentry.Flush(2 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// runMigrationsCLI executes migration commands (sqlite builds only).
func runMigrationsCLI(cmd string) {
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:cloudpam.db?cache=shared&_fk=1"
	}
	switch cmd {
	case "up":
		// initialize store (runs migrations in sqlite build), then show status
		_ = selectStore()
		fallthrough
	case "status":
		status := "migrations status not available in this build"
		if s := sqliteStatus(dsn); s != "" {
			status = s
		}
		log.Println(status)
	default:
		log.Printf("unknown migrate command: %s (use 'up' or 'status')", cmd)
	}
}
