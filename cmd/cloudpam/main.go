package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

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

	// Handle migrations CLI before starting server
	if *migrate != "" {
		runMigrationsCLI(*migrate)
		return
	}

	// Select storage based on build tags and env (see store_*.go in this package).
	store := selectStore()

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
		log.Fatalf("server error: %v", err)
	}

	// Graceful shutdown path (not usually reached in ListenAndServe example)
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
