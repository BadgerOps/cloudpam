package discovery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"syscall"
	"time"

	"github.com/aws/smithy-go"
	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

const syncTimeout = 10 * time.Minute
const syncLeaseDuration = 15 * time.Minute

var ErrSyncBusy = errors.New("a server sync is already running or the schedule is no longer due")

// Sync executes one manual server sync. Scheduled runs use the same credential and retry path.
func (s *SyncService) Sync(ctx context.Context, account domain.Account) (*domain.SyncJob, error) {
	return s.sync(ctx, account, false)
}
func (s *SyncService) sync(ctx context.Context, account domain.Account, scheduled bool) (*domain.SyncJob, error) {
	host, ok := s.collectors[account.Provider]
	if !ok {
		return nil, fmt.Errorf("no collector registered for provider %q", account.Provider)
	}
	now := time.Now().UTC()
	if coord, ok := s.store.(storage.SyncScheduleStore); ok {
		token := uuid.NewString()
		claimed, err := coord.ClaimSync(ctx, account.ID, token, now, now.Add(syncLeaseDuration), scheduled)
		if err != nil {
			return nil, fmt.Errorf("claim sync: %w", err)
		}
		if !claimed {
			return nil, ErrSyncBusy
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := coord.ReleaseSync(cleanup, account.ID, token); err != nil {
				s.logError("release sync lease", err)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	trigger := "manual"
	if scheduled {
		trigger = "scheduled"
	}
	job, err := s.store.CreateSyncJob(ctx, domain.SyncJob{ID: uuid.New(), AccountID: account.ID, Status: domain.SyncJobStatusRunning, Source: "local", Trigger: trigger, IdentitySource: "host", StartedAt: &now, CreatedAt: now})
	if err != nil {
		return nil, fmt.Errorf("create sync job: %w", err)
	}
	finish := func(runErr error) (*domain.SyncJob, error) {
		at := time.Now().UTC()
		job.CompletedAt = &at
		job.Status = domain.SyncJobStatusCompleted
		if runErr != nil {
			job.Status = domain.SyncJobStatusFailed
			job.ErrorMessage = syncErrorMessage("", runErr)
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := s.store.UpdateSyncJob(cleanup, job); err != nil {
			return &job, errors.Join(runErr, fmt.Errorf("save sync result: %w", err))
		}
		return &job, runErr
	}
	rec, stored, err := s.storedCredential(ctx, account.ID)
	if err != nil {
		return finish(fmt.Errorf("credential: %w", err))
	}
	collector := host
	if stored {
		job.IdentitySource = "stored_credential"
		p, err := s.credentials.unseal(rec)
		if err != nil {
			return finish(fmt.Errorf("credential: %w", err))
		}
		collector, err = s.bind(ctx, account, host, p)
		if err != nil {
			return finish(fmt.Errorf("credential: %w", err))
		}
	}
	var resources []domain.DiscoveredResource
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		job.Attempts = attempt
		if err := s.store.UpdateSyncJob(ctx, job); err != nil {
			return finish(fmt.Errorf("save sync attempt: %w", err))
		}
		resources, err = collector.Discover(ctx, account)
		if err == nil {
			break
		}
		if attempt == 3 || !transientDiscoveryError(err) || ctx.Err() != nil {
			break
		}
		delay := time.Second * time.Duration(1<<(attempt-1))
		if s.retryWait != nil {
			err = s.retryWait(ctx, delay)
		} else {
			err = waitRetry(ctx, delay)
		}
		if err != nil {
			return finish(err)
		}
	}
	if err != nil {
		var pe *ProbeError
		if errors.As(err, &pe) {
			return finish(fmt.Errorf("credential: %w", err))
		}
		return finish(err)
	}
	job.ResourcesFound = len(resources)
	job.ResourcesCreated, job.ResourcesUpdated, job.ResourcesDeleted, err = s.ProcessResources(ctx, account.ID, resources, now)
	if err != nil {
		return finish(fmt.Errorf("process resources: %w", err))
	}
	return finish(nil)
}

func waitRetry(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func transientDiscoveryError(err error) bool {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		for _, e := range joined.Unwrap() {
			if !transientDiscoveryError(e) {
				return false
			}
		}
		return len(joined.Unwrap()) > 0
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var pe *ProbeError
	if errors.As(err, &pe) {
		return pe.Status == 429 || pe.Status >= 500 || transientAWSCode(pe.Code)
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return transientAWSCode(ae.ErrorCode()) || ae.ErrorFault() == smithy.FaultServer
	}
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		n := status.HTTPStatusCode()
		return n == 429 || n >= 500
	}
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE)
}

// ProcessResources compares one inventory snapshot with cloud-owned fields. Unchanged
// resources only refresh last_seen_at; failed writes never trigger stale marking.
func (s *SyncService) ProcessResources(ctx context.Context, accountID int64, resources []domain.DiscoveredResource, syncTime time.Time) (created, updated, stale int, err error) {
	seen := make(map[string]bool, len(resources))
	for _, r := range resources {
		if (r.AccountID != 0 && r.AccountID != accountID) || r.ResourceID == "" || seen[r.ResourceID] {
			return 0, 0, 0, fmt.Errorf("invalid or duplicate resource in account snapshot")
		}
		seen[r.ResourceID] = true
	}
	existing := make(map[string]domain.DiscoveredResource)
	for page := 1; ; page++ {
		batch, total, e := s.store.ListDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{Page: page, PageSize: 1000})
		if e != nil {
			return 0, 0, 0, fmt.Errorf("load inventory: %w", e)
		}
		for _, r := range batch {
			existing[r.ResourceID] = r
		}
		if len(batch) == 0 || page*1000 >= total {
			break
		}
	}
	for _, r := range resources {
		r.AccountID = accountID
		r.LastSeenAt = syncTime
		old, exists := existing[r.ResourceID]
		if exists && sameCloudResource(old, r) {
			if e := s.store.TouchDiscoveredResource(ctx, old.ID, syncTime); e != nil {
				return created, updated, 0, e
			}
			continue
		}
		r.UpdatedAt = syncTime
		if exists {
			r.ID = old.ID
			r.DiscoveredAt = old.DiscoveredAt
			r.PoolID = old.PoolID
		} else {
			r.DiscoveredAt = syncTime
		}
		if e := s.store.UpsertDiscoveredResource(ctx, r); e != nil {
			return created, updated, 0, e
		}
		if exists {
			updated++
		} else {
			created++
		}
	}
	stale, err = s.store.MarkStaleResources(ctx, accountID, syncTime)
	return
}
func sameCloudResource(a, b domain.DiscoveredResource) bool {
	parentEqual := a.ParentResourceID == nil && b.ParentResourceID == nil || a.ParentResourceID != nil && b.ParentResourceID != nil && *a.ParentResourceID == *b.ParentResourceID
	return a.Provider == b.Provider && a.Region == b.Region && a.ResourceType == b.ResourceType && a.Name == b.Name && a.CIDR == b.CIDR && a.Status == b.Status && parentEqual && maps.Equal(a.Metadata, b.Metadata)
}

// HTTPError exposes a retryable status without retaining provider response bodies.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string       { return fmt.Sprintf("cloud request failed: http %d", e.Status) }
func (e *HTTPError) HTTPStatusCode() int { return e.Status }

func transientAWSCode(code string) bool {
	switch code {
	case "Throttling", "ThrottlingException", "RequestLimitExceeded", "TooManyRequestsException", "ServiceUnavailable", "InternalError", "InternalFailure":
		return true
	}
	return false
}
