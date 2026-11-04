package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/oauth2/google"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

// gcpOrgDeps are the cloud calls a GCP org pass makes, injectable for tests.
type gcpOrgDeps struct {
	listProjects func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error)
	collectorFor func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error)
}

// defaultGCPOrgDeps lists projects and reads them as the agent's Application
// Default Credentials. With an impersonation template set, each project is read
// through the service account the template names instead.
func defaultGCPOrgDeps(cfg *Config) gcpOrgDeps {
	template := cfg.GCPOrg.ImpersonateServiceAccount
	var iamClient *http.Client
	return gcpOrgDeps{
		listProjects: func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error) {
			client, err := google.DefaultClient(ctx, gcp.ResourceManagerScope)
			if err != nil {
				return nil, fmt.Errorf("gcp default credentials: %w", err)
			}
			return gcp.ListProjects(ctx, client, parent, limiter)
		},
		collectorFor: func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error) {
			if template == "" {
				c := gcp.New()
				c.SetLimiter(limiter)
				return c, nil
			}
			if iamClient == nil {
				client, err := google.DefaultClient(ctx, gcp.IAMCredentialsScope)
				if err != nil {
					return nil, fmt.Errorf("gcp default credentials: %w", err)
				}
				iamClient = client
			}
			ts := gcp.ImpersonatedTokenSource(ctx, iamClient, gcp.ServiceAccountFor(template, project), limiter)
			c := gcp.NewWithTokenSource(ts)
			c.SetLimiter(limiter)
			return c, nil
		},
	}
}

// runGCPOrgSync discovers every active project under the configured parent and
// bulk-ingests them. A project that cannot be read is skipped; a listing
// failure pushes nothing, so the next tick retries the whole pass.
func runGCPOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger, deps gcpOrgDeps) {
	parent := cfg.GCPOrg.Parent
	logger.Info("starting gcp org discovery sync", "parent", parent,
		"impersonation", cfg.GCPOrg.ImpersonateServiceAccount != "")

	// One limiter for the pass: Resource Manager, IAM Credentials and every
	// project's Compute calls draw from the same budget.
	limiter := discovery.NewLimiter(cfg.DiscoveryGCPRPS)

	projects, err := deps.listProjects(ctx, parent, limiter)
	if err != nil {
		logger.Error("failed to list gcp projects", "parent", parent, "error", err)
		return
	}

	exclude := make(map[string]bool, len(cfg.GCPOrg.ExcludeProjects))
	for _, id := range cfg.GCPOrg.ExcludeProjects {
		exclude[id] = true
	}
	regions := cfg.GCPOrg.Regions
	if len(regions) == 0 {
		regions = cfg.GCPRegions
	}

	var entries []domain.OrgAccountIngest
	for _, project := range projects {
		if exclude[project.ID] {
			logger.Info("excluding project", "project", project.ID)
			continue
		}
		collector, err := deps.collectorFor(ctx, project.ID, limiter)
		if err != nil {
			logger.Error("gcp credentials failed for project", "project", project.ID, "error", err)
			continue
		}
		account := domain.Account{Provider: providerGCP, ExternalID: project.ID, Regions: regions}
		resources, err := collector.Discover(ctx, account)
		if err != nil {
			// Partial results are dropped: pushing them would mark the missing
			// resources stale.
			logger.Error("discovery failed for project", "project", project.ID, "error", err)
			continue
		}
		logger.Info("discovered resources for project", "project", project.ID, "count", len(resources))

		name := project.Name
		if name == "" {
			name = project.ID
		}
		entries = append(entries, domain.OrgAccountIngest{
			Provider:    providerGCP,
			ExternalID:  project.ID,
			AccountName: name,
			Regions:     regions,
			Resources:   resources,
		})
	}

	if len(entries) == 0 {
		logger.Info("no gcp projects to ingest", "parent", parent)
		return
	}

	req := domain.BulkIngestRequest{Accounts: entries, SyncJobID: syncJobID}
	if err := pusher.PushOrgResources(ctx, req, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push gcp org resources failed", "error", err)
		return
	}
	logger.Info("gcp org sync completed", "projects", len(entries))
}
