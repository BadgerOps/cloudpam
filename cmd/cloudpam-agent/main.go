package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/aws"
	"cloudpam/internal/domain"
)

const version = "dev"

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file (optional, can use env vars)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load configuration
	cfg, err := LoadConfig(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Info("cloudpam-agent starting",
		"version", version,
		"server_url", cfg.ServerURL,
		"account_id", cfg.AccountID,
		"agent_name", cfg.AgentName,
		"sync_interval", cfg.SyncInterval,
		"heartbeat_interval", cfg.HeartbeatInterval,
	)

	// Generate agent ID
	agentID := uuid.New()
	logger.Info("agent id generated", "agent_id", agentID)

	// Get hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Create pusher
	pusher := NewPusher(cfg.ServerURL, cfg.APIKey, agentID, cfg.RequestTimeout, logger)

	// Create AWS collector
	collector := aws.New()

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run scheduler
	if err := runScheduler(ctx, cfg, collector, pusher, agentID, hostname, logger); err != nil {
		logger.Error("scheduler failed", "error", err)
		os.Exit(1)
	}

	logger.Info("cloudpam-agent stopped")
}

func runScheduler(ctx context.Context, cfg *Config, collector discovery.Collector, pusher *Pusher, agentID uuid.UUID, hostname string, logger *slog.Logger) error {
	syncTicker := time.NewTicker(cfg.SyncInterval)
	defer syncTicker.Stop()

	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Initial sync and heartbeat
	runSync(ctx, cfg, collector, pusher, logger)
	_ = pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname)

	for {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping scheduler")
			return nil

		case <-syncTicker.C:
			runSync(ctx, cfg, collector, pusher, logger)

		case <-heartbeatTicker.C:
			if err := pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname); err != nil {
				logger.Error("heartbeat failed", "error", err)
			}
		}
	}
}

func runSync(ctx context.Context, cfg *Config, collector discovery.Collector, pusher *Pusher, logger *slog.Logger) {
	logger.Info("starting discovery sync", "account_id", cfg.AccountID)

	// Create mock account with AWS regions
	account := domain.Account{
		ID:       cfg.AccountID,
		Provider: "aws",
		Regions:  cfg.AWSRegions,
	}

	// Discover resources
	resources, err := collector.Discover(ctx, account)
	if err != nil {
		logger.Error("discovery failed", "error", err)
		return
	}

	logger.Info("discovered resources", "count", len(resources))

	// Push to server with retry
	if err := pusher.PushResources(ctx, account.ID, resources, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push resources failed", "error", err)
		return
	}

	logger.Info("sync completed successfully", "account_id", account.ID)
}
