package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/aws"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

var version = "dev"

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
		"provider", cfg.Provider,
		"account_id", cfg.AccountID,
		"agent_name", cfg.AgentName,
		"sync_interval", cfg.SyncInterval,
		"heartbeat_interval", cfg.HeartbeatInterval,
	)

	agentID, err := cfg.ResolveAgentID()
	if err != nil {
		logger.Error("failed to resolve agent id", "error", err)
		os.Exit(1)
	}
	logger.Info("agent id resolved", "agent_id", agentID)

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

	collector, err := newSingleAccountCollector(cfg)
	if err != nil {
		logger.Error("failed to create collector", "error", err)
		os.Exit(1)
	}

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
	syncFn := func(syncJobID *uuid.UUID) { runSync(ctx, cfg, collector, pusher, syncJobID, logger) }
	switch {
	case cfg.AWSOrg.Enabled:
		logger.Info("aws org mode enabled", "role_name", cfg.AWSOrg.RoleName, "regions", cfg.AWSOrg.Regions)
		syncFn = func(syncJobID *uuid.UUID) { runAWSOrgSync(ctx, cfg, pusher, syncJobID, logger) }
	case cfg.GCPOrg.Enabled:
		logger.Info("gcp org mode enabled", "parent", cfg.GCPOrg.Parent, "regions", cfg.GCPOrg.Regions)
		syncFn = func(syncJobID *uuid.UUID) {
			runGCPOrgSync(ctx, cfg, pusher, syncJobID, logger, defaultGCPOrgDeps(cfg))
		}
	}

	// Initial sync and heartbeat. Org-mode agents still need to appear as live
	// agents in the server UI even though discovery itself is pushed in bulk.
	syncFn(nil)
	if cfg.AccountID > 0 {
		_, _ = pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping scheduler")
			return nil

		case <-syncTicker.C:
			syncFn(nil)

		case <-heartbeatTicker.C:
			if cfg.AccountID > 0 {
				resp, err := pusher.Heartbeat(ctx, cfg.AgentName, cfg.AccountID, version, hostname)
				if err != nil {
					logger.Error("heartbeat failed", "error", err)
					continue
				}
				if resp != nil && resp.SyncJobID != nil {
					logger.Info("received server-requested sync", "sync_job_id", resp.SyncJobID, "account_id", resp.AccountID)
					syncFn(resp.SyncJobID)
				}
			}
		}
	}
}

func runAWSOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger) {
	logger.Info("starting org discovery sync")

	// One limiter for the whole pass, so every member account's EC2 calls
	// draw from the same budget.
	limiter := discovery.NewLimiter(cfg.DiscoveryAWSRPS)

	// Enumerate all active accounts in the organization
	orgAccounts, err := aws.ListOrgAccounts(ctx, limiter)
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
		collector.SetLimiter(limiter)

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
			ExternalID:   orgAcct.ID,
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
		Accounts:  ingestAccounts,
		SyncJobID: syncJobID,
	}

	if err := pusher.PushOrgResources(ctx, req, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push org resources failed", "error", err)
		return
	}

	logger.Info("org sync completed", "accounts", len(ingestAccounts))
}

func runSync(ctx context.Context, cfg *Config, collector discovery.Collector, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger) {
	logger.Info("starting discovery sync", "account_id", cfg.AccountID, "provider", cfg.Provider)

	account := singleAccount(cfg)

	// Discover resources
	resources, err := collector.Discover(ctx, account)
	if err != nil {
		logger.Error("discovery failed", "error", err)
		return
	}

	logger.Info("discovered resources", "count", len(resources))

	// Push to server with retry
	if err := pusher.PushResources(ctx, account.ID, resources, syncJobID, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push resources failed", "error", err)
		return
	}

	logger.Info("sync completed successfully", "account_id", account.ID)
}

// newSingleAccountCollector returns the collector for single-account mode.
func newSingleAccountCollector(cfg *Config) (discovery.Collector, error) {
	switch cfg.Provider {
	case providerAWS:
		c := aws.New()
		c.SetLimiter(discovery.NewLimiter(cfg.DiscoveryAWSRPS))
		return c, nil
	case providerGCP:
		c := gcp.New()
		c.SetLimiter(discovery.NewLimiter(cfg.DiscoveryGCPRPS))
		c.SetRequestTimeout(cfg.RequestTimeout)
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

// singleAccount is the account single-account mode discovers. The server owns
// the real record; the agent only needs what its collector reads.
func singleAccount(cfg *Config) domain.Account {
	account := domain.Account{ID: cfg.AccountID, Provider: cfg.Provider}
	switch cfg.Provider {
	case providerAWS:
		account.Regions = cfg.AWSRegions
	case providerGCP:
		account.ExternalID = cfg.GCPProject
		account.Regions = cfg.GCPRegions
	}
	return account
}
