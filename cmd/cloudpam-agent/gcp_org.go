package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

// gcpProjectTimeout bounds one project's credentials and discovery, so a
// stalled project cannot hold up the rest of the pass.
var gcpProjectTimeout = 10 * time.Minute

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
	timeout := cfg.RequestTimeout
	var iamClient *http.Client
	return gcpOrgDeps{
		listProjects: func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error) {
			client, err := gcpDefaultClient(ctx, gcp.ResourceManagerScope, timeout)
			if err != nil {
				return nil, err
			}
			return gcp.ListProjects(ctx, client, parent, limiter)
		},
		collectorFor: func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error) {
			if template == "" {
				c := gcp.New()
				c.SetLimiter(limiter)
				c.SetRequestTimeout(timeout)
				return c, nil
			}
			if iamClient == nil {
				// The client outlives this project, so its token refreshes must
				// not inherit the project's deadline; the client timeout bounds them.
				client, err := gcpDefaultClient(context.WithoutCancel(ctx), gcp.IAMCredentialsScope, timeout)
				if err != nil {
					return nil, err
				}
				iamClient = client
			}
			ts := gcp.ImpersonatedTokenSource(ctx, iamClient, gcp.ServiceAccountFor(template, project), limiter)
			c := gcp.NewWithTokenSource(ts)
			c.SetLimiter(limiter)
			c.SetRequestTimeout(timeout)
			return c, nil
		},
	}
}

// gcpDefaultClient is an Application Default Credentials client whose requests
// and token refreshes each give up after timeout.
func gcpDefaultClient(ctx context.Context, scope string, timeout time.Duration) (*http.Client, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: timeout})
	client, err := google.DefaultClient(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("gcp default credentials: %w", err)
	}
	client.Timeout = timeout
	return client, nil
}

// runGCPOrgSync discovers every active project under the configured parent and
// bulk-ingests them. A project that cannot be read is reported as a failure
// rather than ingested, so its inventory stays as it was; a listing failure is
// reported the same way for the parent.
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
		pushOrg(ctx, cfg, pusher, logger, domain.BulkIngestRequest{
			SyncJobID: syncJobID,
			Failures:  []domain.OrgAccountFailure{domain.NewOrgAccountFailure(providerGCP, parent, fmt.Errorf("list projects: %w", err))},
		})
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
	var failures []domain.OrgAccountFailure
	for _, project := range projects {
		if exclude[project.ID] {
			logger.Info("excluding project", "project", project.ID)
			continue
		}
		projectCtx, cancel := context.WithTimeout(ctx, gcpProjectTimeout)
		collector, err := deps.collectorFor(projectCtx, project.ID, limiter)
		if err != nil {
			cancel()
			logger.Error("gcp credentials failed for project", "project", project.ID, "error", err)
			failures = append(failures, domain.NewOrgAccountFailure(providerGCP, project.ID, fmt.Errorf("credentials: %w", err)))
			continue
		}
		account := domain.Account{Provider: providerGCP, ExternalID: project.ID, Regions: regions}
		resources, err := collector.Discover(projectCtx, account)
		cancel()
		if err != nil {
			// Partial results are dropped: pushing them would mark the missing
			// resources stale.
			logger.Error("discovery failed for project", "project", project.ID, "error", err)
			failures = append(failures, domain.NewOrgAccountFailure(providerGCP, project.ID, err))
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

	if len(entries) == 0 && len(failures) == 0 {
		logger.Info("no gcp projects to ingest", "parent", parent)
		return
	}
	pushOrg(ctx, cfg, pusher, logger, domain.BulkIngestRequest{Accounts: entries, Failures: failures, SyncJobID: syncJobID})
}
