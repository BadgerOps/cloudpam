// Package discovery provides cloud resource discovery and sync capabilities.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// Collector discovers cloud resources for a given account.
type Collector interface {
	// Provider returns the cloud provider name (e.g., "aws", "gcp", "azure").
	Provider() string
	// Discover returns all discovered resources for the given account.
	Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error)
}

// CredentialAware is implemented by host-identity collectors that can also run as a stored credential.
type CredentialAware interface {
	WithCredential(ctx context.Context, account domain.Account, cred CredentialPayload) (CredentialedCollector, error)
}

// CredentialedCollector is a collector bound to one stored credential.
type CredentialedCollector interface {
	Collector
	Identity(ctx context.Context, account domain.Account) (string, error)
}

// ProbeStep names the provider call that failed.
type ProbeStep string

const (
	ProbeStepAssumeRole        ProbeStep = "assume_role"
	ProbeStepGetCallerIdentity ProbeStep = "get_caller_identity"
	ProbeStepToken             ProbeStep = "token"
	ProbeStepGetProject        ProbeStep = "get_project"
)

// ProbeError is a provider failure reduced to fields safe to store and show.
type ProbeError struct {
	Step   ProbeStep
	Status int
	Code   string
}

func (e *ProbeError) Error() string {
	code := clipProbeCode(e.Code)
	switch {
	case code != "" && e.Status != 0:
		return fmt.Sprintf("%s: %s (http %d)", e.Step, code, e.Status)
	case code != "":
		return string(e.Step) + ": " + code
	case e.Status != 0:
		return fmt.Sprintf("%s: http %d", e.Step, e.Status)
	default:
		return string(e.Step) + ": failed"
	}
}

const maxProbeCodeLength = 64

func clipProbeCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if b.Len() == maxProbeCodeLength {
			break
		}
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

const credentialTestTimeout = 15 * time.Second

// SyncService orchestrates discovery sync runs.
type SyncService struct {
	store       storage.DiscoveryStore
	collectors  map[string]Collector
	credentials *AccountCredentials
}

// SetCredentials makes Sync and TestCredential honour stored per-account credentials.
func (s *SyncService) SetCredentials(c *AccountCredentials) {
	s.credentials = c
}

func (s *SyncService) collectorFor(ctx context.Context, account domain.Account, host Collector) (Collector, error) {
	if s.credentials == nil {
		return host, nil
	}
	rec, err := s.credentials.load(ctx, account.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return host, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := s.credentials.unseal(rec)
	if err != nil {
		return nil, err
	}
	return s.bind(ctx, account, host, p)
}

func (s *SyncService) bind(ctx context.Context, account domain.Account, host Collector, p CredentialPayload) (CredentialedCollector, error) {
	if err := checkProvider(p, account); err != nil {
		return nil, err
	}
	aware, ok := host.(CredentialAware)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrCredentialUnsupported, account.Provider)
	}
	return aware.WithCredential(ctx, account, p)
}

// TestCredential runs one read-only call as the stored credential and records the outcome.
// A failing credential is a result with Status failed, not an error. storage.ErrNotFound
// when nothing is stored.
func (s *SyncService) TestCredential(ctx context.Context, account domain.Account) (domain.CredentialTestResult, error) {
	if s.credentials == nil {
		return domain.CredentialTestResult{}, storage.ErrNotFound
	}
	rec, err := s.credentials.load(ctx, account.ID)
	if err != nil {
		return domain.CredentialTestResult{}, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, credentialTestTimeout)
	defer cancel()
	identity, probeErr := s.probe(probeCtx, account, rec)

	result := domain.CredentialTestResult{
		CredentialTest: domain.CredentialTest{TestedAt: time.Now().UTC(), Status: domain.CredentialTestOK},
		Identity:       identity,
	}
	if probeErr != nil {
		result.Status = domain.CredentialTestFailed
		result.Error = diagnoseProbe(probeErr)
		result.Identity = ""
	}

	if ctx.Err() != nil {
		return result, nil
	}
	switch err := s.credentials.recordTest(ctx, rec, result.CredentialTest); {
	case errors.Is(err, storage.ErrConflict):
		slog.Default().Info("credential test result dropped: credential replaced during test", "account_id", account.ID)
	case err != nil:
		return domain.CredentialTestResult{}, fmt.Errorf("record credential test: %w", err)
	}
	return result, nil
}

func (s *SyncService) probe(ctx context.Context, account domain.Account, rec domain.AccountCredentialRecord) (string, error) {
	p, err := s.credentials.unseal(rec)
	if err != nil {
		return "", err
	}
	bound, err := s.bind(ctx, account, s.collectors[account.Provider], p)
	if err != nil {
		return "", err
	}
	return bound.Identity(ctx, account)
}

var credentialSentinels = []error{ErrCredentialUnreadable, ErrCredentialProviderMismatch, ErrCredentialUnsupported}

func diagnoseProbe(err error) string {
	var pe *ProbeError
	var netErr net.Error
	switch {
	case errors.As(err, &pe):
		return pe.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("timed out after %s", credentialTestTimeout)
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &netErr):
		return "endpoint unreachable"
	}
	for _, sentinel := range credentialSentinels {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return "failed"
}

// NewSyncService creates a new sync service with the given discovery store.
func NewSyncService(store storage.DiscoveryStore) *SyncService {
	return &SyncService{
		store:      store,
		collectors: make(map[string]Collector),
	}
}

// RegisterCollector registers a collector for a cloud provider.
func (s *SyncService) RegisterCollector(c Collector) {
	s.collectors[c.Provider()] = c
}

// Sync runs a discovery sync for the given account.
// It creates a SyncJob, runs the appropriate collector, upserts resources,
// and marks stale resources.
func (s *SyncService) Sync(ctx context.Context, account domain.Account) (*domain.SyncJob, error) {
	provider := account.Provider
	collector, ok := s.collectors[provider]
	if !ok {
		return nil, fmt.Errorf("no collector registered for provider %q", provider)
	}

	now := time.Now().UTC()
	job := domain.SyncJob{
		ID:        uuid.New(),
		AccountID: account.ID,
		Status:    domain.SyncJobStatusRunning,
		Source:    "local",
		StartedAt: &now,
		CreatedAt: now,
	}
	job, err := s.store.CreateSyncJob(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("create sync job: %w", err)
	}

	collector, err = s.collectorFor(ctx, account, collector)
	if err != nil {
		completedAt := time.Now().UTC()
		job.Status = domain.SyncJobStatusFailed
		job.CompletedAt = &completedAt
		job.ErrorMessage = "credential: " + err.Error()
		_ = s.store.UpdateSyncJob(ctx, job)
		return &job, fmt.Errorf("credential: %w", err)
	}

	// Run discovery
	resources, discoverErr := collector.Discover(ctx, account)

	if discoverErr != nil {
		completedAt := time.Now().UTC()
		job.Status = domain.SyncJobStatusFailed
		job.CompletedAt = &completedAt
		job.ErrorMessage = discoverErr.Error()
		_ = s.store.UpdateSyncJob(ctx, job)
		return &job, fmt.Errorf("discovery failed: %w", discoverErr)
	}

	// Process resources (upsert and mark stale)
	created, updated, staleCount, processErr := s.ProcessResources(ctx, account.ID, resources, now)

	completedAt := time.Now().UTC()
	job.CompletedAt = &completedAt
	job.ResourcesFound = len(resources)
	job.ResourcesCreated = created
	job.ResourcesUpdated = updated
	job.ResourcesDeleted = staleCount
	if processErr != nil {
		job.Status = domain.SyncJobStatusFailed
		job.ErrorMessage = processErr.Error()
		_ = s.store.UpdateSyncJob(ctx, job)
		return &job, fmt.Errorf("process resources: %w", processErr)
	}
	job.Status = domain.SyncJobStatusCompleted
	_ = s.store.UpdateSyncJob(ctx, job)

	return &job, nil
}

// ProcessResources upserts discovered resources and marks stale resources.
// This is shared logic used by both local sync and agent ingest.
// Returns created, updated, stale counts and any error.
func (s *SyncService) ProcessResources(
	ctx context.Context,
	accountID int64,
	resources []domain.DiscoveredResource,
	syncTime time.Time,
) (created, updated, stale int, err error) {
	// Upsert discovered resources
	for _, res := range resources {
		// Check if resource already exists
		existing, findErr := s.findByAccountAndResourceID(ctx, accountID, res.ResourceID)
		if findErr != nil && err == nil {
			err = fmt.Errorf("lookup existing resource %q: %w", res.ResourceID, findErr)
		}
		if existing != nil {
			updated++
		} else {
			created++
		}
		if upsertErr := s.store.UpsertDiscoveredResource(ctx, res); upsertErr != nil {
			// Log and continue — don't fail the entire sync
			if err == nil {
				err = fmt.Errorf("upsert errors: %w", upsertErr)
			}
		}
	}

	// Mark resources not seen in this run as stale
	staleCount, markErr := s.store.MarkStaleResources(ctx, accountID, syncTime)
	if markErr != nil && err == nil {
		err = fmt.Errorf("mark stale: %w", markErr)
	}

	return created, updated, staleCount, err
}

// findByAccountAndResourceID checks if a resource exists (helper for counting creates vs updates).
func (s *SyncService) findByAccountAndResourceID(ctx context.Context, accountID int64, resourceID string) (*domain.DiscoveredResource, error) {
	const pageSize = 1000
	for page := 1; ; page++ {
		resources, total, err := s.store.ListDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range resources {
			if r.ResourceID == resourceID {
				return &r, nil
			}
		}
		if len(resources) == 0 || page*pageSize >= total {
			return nil, nil
		}
	}
}
