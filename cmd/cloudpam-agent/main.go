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

	// Register with server if bootstrapped from a provisioning token
	if cfg.Bootstrapped {
		logger.Info("registering with server (bootstrap token)")
		regResp, err := pusher.Register(context.Background(), cfg.AgentName, cfg.AccountID, version, hostname)
		if err != nil {
			logger.Error("agent registration failed", "error", err)
			os.Exit(1)
		}
		logger.Info("agent registered",
			"agent_id", regResp.AgentID,
			"approval_status", regResp.ApprovalStatus,
			"message", regResp.Message,
		)
	}

	// Create AWS collector
	collector := aws.New()

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Run scheduler (org mode or single-account mode)
	if err := runScheduler(ctx, cfg, collector, pusher, agentID, hostname, logger); err != nil {
		logger.Error("scheduler failed", "error", err)
		cancel()
		os.Exit(1)
	}

	cancel()
	logger.Info("cloudpam-agent stopped")
}

func runScheduler(ctx context.Context, cfg *Config, collector discovery.Collector, pusher *Pusher, agentID uuid.UUID, hostname string, logger *slog.Logger) error {
	syncTicker := time.NewTicker(cfg.SyncInterval)
	defer syncTicker.Stop()

	heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Choose sync function based on mode
	syncFn := func() { runSync(ctx, cfg, collector, pusher, logger) }
	if cfg.AWSOrg.Enabled {
		logger.Info("org mode enabled", "role_name", cfg.AWSOrg.RoleName, "regions", cfg.AWSOrg.Regions)
		syncFn = func() { runOrgSync(ctx, cfg, pusher, logger) }
	}

	// Initial sync and heartbeat
	syncFn()
	if !cfg.AWSOrg.Enabled {
		_ = pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping scheduler")
			return nil

		case <-syncTicker.C:
			syncFn()

		case <-heartbeatTicker.C:
			if !cfg.AWSOrg.Enabled {
				if err := pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname); err != nil {
					logger.Error("heartbeat failed", "error", err)
				}
			}
		}
	}
}

func runOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, logger *slog.Logger) {
	logger.Info("starting org discovery sync")

	// Enumerate all active accounts in the organization
	orgAccounts, err := aws.ListOrgAccounts(ctx)
	if err != nil {
		logger.Error("failed to list org accounts", "error", err)
		return
	}

	// Build exclude set
	excludeSet := make(map[string]bool, len(cfg.AWSOrg.ExcludeAccounts))
	for _, id := range cfg.AWSOrg.ExcludeAccounts {
		excludeSet[id] = true
	}

	regions := cfg.AWSOrg.Regions
	if len(regions) == 0 {
		regions = cfg.AWSRegions
	}

	var ingestAccounts []domain.OrgAccountIngest

	for _, orgAcct := range orgAccounts {
		if excludeSet[orgAcct.ID] {
			logger.Info("excluding account", "account_id", orgAcct.ID, "name", orgAcct.Name)
			continue
		}

		logger.Info("discovering account", "account_id", orgAcct.ID, "name", orgAcct.Name)

		// Assume role into member account
		creds, err := aws.AssumeRole(ctx, orgAcct.ID, cfg.AWSOrg.RoleName, cfg.AWSOrg.ExternalID)
		if err != nil {
			logger.Error("assume role failed", "account_id", orgAcct.ID, "error", err)
			continue
		}

		// Create collector with assumed credentials
		collector := aws.NewWithCredentials(creds)

		// Build a temporary account for discovery
		account := domain.Account{
			Provider: "aws",
			Regions:  regions,
		}

		resources, err := collector.Discover(ctx, account)
		if err != nil {
			logger.Error("discovery failed for account", "account_id", orgAcct.ID, "error", err)
			continue
		}

		logger.Info("discovered resources for account", "account_id", orgAcct.ID, "count", len(resources))

		ingestAccounts = append(ingestAccounts, domain.OrgAccountIngest{
			AWSAccountID: orgAcct.ID,
			AccountName:  orgAcct.Name,
			AccountEmail: orgAcct.Email,
			Provider:     "aws",
			Regions:      regions,
			Resources:    resources,
		})
	}

	if len(ingestAccounts) == 0 {
		logger.Info("no accounts to ingest")
		return
	}

	req := domain.BulkIngestRequest{
		Accounts: ingestAccounts,
	}

	if err := pusher.PushOrgResources(ctx, req, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push org resources failed", "error", err)
		return
	}

	logger.Info("org sync completed", "accounts", len(ingestAccounts))
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
