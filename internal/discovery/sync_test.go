package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
	"github.com/google/uuid"
)

type sequenceCollector struct {
	calls    int
	failures []error
	run      func(context.Context) error
}

func (c *sequenceCollector) Provider() string { return "aws" }
func (c *sequenceCollector) Discover(ctx context.Context, _ domain.Account) ([]domain.DiscoveredResource, error) {
	c.calls++
	if c.run != nil {
		return nil, c.run(ctx)
	}
	if c.calls <= len(c.failures) {
		return nil, c.failures[c.calls-1]
	}
	return nil, nil
}
func TestSyncRetryPolicyAndJobMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures []error
		attempts int
		failed   bool
	}{
		{"transient", []error{&HTTPError{503}, &HTTPError{429}}, 3, false},
		{"exhausted", []error{&HTTPError{503}, &HTTPError{503}, &HTTPError{503}}, 3, true},
		{"permission", []error{&HTTPError{403}}, 1, true},
		{"throttled credential", []error{&ProbeError{Status: 400, Code: "Throttling"}}, 2, false},
		{"request timeout", []error{context.DeadlineExceeded}, 2, false},
		{"canceled", []error{context.Canceled}, 1, true},
		{"mixed failure", []error{errors.Join(&HTTPError{503}, &HTTPError{403})}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
			svc := NewSyncService(ds)
			c := &sequenceCollector{failures: tc.failures}
			svc.RegisterCollector(c)
			var waits []time.Duration
			svc.retryWait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
			job, err := svc.Sync(context.Background(), domain.Account{ID: 1, Provider: "aws"})
			if (err != nil) != tc.failed || job.Attempts != tc.attempts || c.calls != tc.attempts {
				t.Fatalf("job=%+v calls=%d error=%v", job, c.calls, err)
			}
			if job.IdentitySource != "host" || job.Source != "local" || job.Trigger != "manual" {
				t.Fatalf("metadata: %+v", job)
			}
			if tc.attempts == 3 && (len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second) {
				t.Fatalf("backoff: %v", waits)
			}
			saved, err := ds.GetSyncJob(context.Background(), job.ID)
			if err != nil || saved.Attempts != tc.attempts || saved.Status != job.Status {
				t.Fatalf("saved=%+v %v", saved, err)
			}
		})
	}
}
func TestSyncCancellationFinalizesAndReleasesLease(t *testing.T) {
	ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
	svc := NewSyncService(ds)
	ctx, cancel := context.WithCancel(context.Background())
	svc.RegisterCollector(&sequenceCollector{run: func(ctx context.Context) error { cancel(); return ctx.Err() }})
	job, err := svc.Sync(ctx, domain.Account{ID: 1, Provider: "aws"})
	if !errors.Is(err, context.Canceled) || job.Status != domain.SyncJobStatusFailed {
		t.Fatalf("%+v %v", job, err)
	}
	saved, err := ds.GetSyncJob(context.Background(), job.ID)
	if err != nil || saved.Status != domain.SyncJobStatusFailed {
		t.Fatalf("%+v %v", saved, err)
	}
	now := time.Now()
	ok, err := ds.ClaimSync(context.Background(), 1, "next", now, now.Add(time.Minute), false)
	if err != nil || !ok {
		t.Fatalf("lease not released: %v %v", ok, err)
	}
}
func TestScheduledSyncSkipsAgentAndUsesPersistedDueTime(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	svc := NewSyncService(ds)
	c := &sequenceCollector{}
	svc.RegisterCollector(c)
	account, err := ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123456789012", Provider: "aws", Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := ds.PutSyncSchedule(ctx, account.ID, 60, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	agent := domain.DiscoveryAgent{ID: uuid.New(), AccountID: account.ID, LastSeenAt: now, ApprovalStatus: domain.AgentApprovalApproved}
	if err := ds.UpsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := svc.runScheduled(ctx, ms, now); err != nil {
		t.Fatal(err)
	}
	if c.calls != 0 {
		t.Fatal("ran for agent-owned account")
	}
	agent.LastSeenAt = now.Add(-6 * time.Minute)
	if err := ds.UpsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := svc.runScheduled(ctx, ms, now); err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Fatalf("calls=%d", c.calls)
	}
	if err := svc.runScheduled(ctx, ms, now); err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 {
		t.Fatal("replayed due run")
	}
	jobs, err := ds.ListSyncJobs(ctx, account.ID, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Trigger != "scheduled" {
		t.Fatalf("jobs=%+v %v", jobs, err)
	}
}
func TestIncrementalResourcePersistence(t *testing.T) {
	ctx := context.Background()
	ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
	svc := NewSyncService(ds)
	now := time.Now().UTC().Truncate(time.Second)
	r := domain.DiscoveredResource{AccountID: 1, ResourceID: "vpc-1", Provider: "aws", ResourceType: domain.ResourceTypeVPC, Name: "same", Status: domain.DiscoveryStatusActive, Metadata: map[string]string{"key": "value"}}
	created, updated, _, err := svc.ProcessResources(ctx, 1, []domain.DiscoveredResource{r}, now)
	if err != nil || created != 1 || updated != 0 {
		t.Fatalf("first: %d %d %v", created, updated, err)
	}
	items, _, _ := ds.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	old := items[0]
	if err := ds.LinkResourceToPool(ctx, old.ID, 7); err != nil {
		t.Fatal(err)
	}
	created, updated, stale, err := svc.ProcessResources(ctx, 1, []domain.DiscoveredResource{r}, now.Add(time.Minute))
	if err != nil || created != 0 || updated != 0 || stale != 0 {
		t.Fatalf("unchanged: %d %d %d %v", created, updated, stale, err)
	}
	got, _ := ds.GetDiscoveredResource(ctx, old.ID)
	if !got.UpdatedAt.Equal(now) || !got.LastSeenAt.Equal(now.Add(time.Minute)) || got.PoolID == nil || *got.PoolID != 7 {
		t.Fatalf("unchanged timestamps/link: %+v", got)
	}
	r.Name = "changed"
	_, updated, _, err = svc.ProcessResources(ctx, 1, []domain.DiscoveredResource{r}, now.Add(2*time.Minute))
	if err != nil || updated != 1 {
		t.Fatalf("changed: %d %v", updated, err)
	}
	got, _ = ds.GetDiscoveredResource(ctx, old.ID)
	if !got.UpdatedAt.Equal(now.Add(2*time.Minute)) || !got.DiscoveredAt.Equal(now) {
		t.Fatalf("changed timestamps: %+v", got)
	}
	_, _, stale, err = svc.ProcessResources(ctx, 1, nil, now.Add(3*time.Minute))
	if err != nil || stale != 1 {
		t.Fatalf("missing: %d %v", stale, err)
	}
	_, updated, _, err = svc.ProcessResources(ctx, 1, []domain.DiscoveredResource{r}, now.Add(4*time.Minute))
	if err != nil || updated != 1 {
		t.Fatalf("reappeared: %d %v", updated, err)
	}
}

type failedUpsertStore struct{ *storage.MemoryDiscoveryStore }

func (s failedUpsertStore) UpsertDiscoveredResource(context.Context, domain.DiscoveredResource) error {
	return errors.New("disk full")
}
func TestIncrementalFailureDoesNotMarkResourcesStale(t *testing.T) {
	ctx := context.Background()
	ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
	now := time.Now().UTC()
	old := domain.DiscoveredResource{AccountID: 1, ResourceID: "old", Status: domain.DiscoveryStatusActive, LastSeenAt: now.Add(-time.Hour)}
	if err := ds.UpsertDiscoveredResource(ctx, old); err != nil {
		t.Fatal(err)
	}
	svc := NewSyncService(failedUpsertStore{ds})
	_, _, _, err := svc.ProcessResources(ctx, 1, []domain.DiscoveredResource{{AccountID: 1, ResourceID: "new"}}, now)
	if err == nil {
		t.Fatal("expected failure")
	}
	items, _, _ := ds.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	if len(items) != 1 || items[0].Status != domain.DiscoveryStatusActive {
		t.Fatalf("marked stale after failed write: %+v", items)
	}
}

func TestSchedulerRecognizesOrganizationAgentOwnership(t *testing.T) {
	now := time.Now().UTC()
	agent := domain.DiscoveryAgent{ID: uuid.New(), AccountID: 100, LastSeenAt: now, ApprovalStatus: domain.AgentApprovalApproved}
	account := domain.Account{ID: 200, Provider: "aws", Key: "aws:123456789012"}
	if agentOwnsAccount([]domain.DiscoveryAgent{agent}, account, "", now) {
		t.Fatal("unrelated agent owns account")
	}
	if !agentOwnsAccount([]domain.DiscoveryAgent{agent}, account, agent.ID.String(), now) {
		t.Fatal("organization owner not recognized")
	}
	agent.ApprovalStatus = domain.AgentApprovalRejected
	if agentOwnsAccount([]domain.DiscoveryAgent{agent}, account, agent.ID.String(), now) {
		t.Fatal("rejected agent owns account")
	}
}

func TestSyncRejectsConcurrentServerRun(t *testing.T) {
	ctx := context.Background()
	ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
	svc := NewSyncService(ds)
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	svc.RegisterCollector(&sequenceCollector{run: func(context.Context) error { close(started); <-release; return nil }})
	account := domain.Account{ID: 1, Provider: "aws"}
	go func() { _, err := svc.Sync(ctx, account); finished <- err }()
	<-started
	job, err := svc.Sync(ctx, account)
	close(release)
	if !errors.Is(err, ErrSyncBusy) || job != nil {
		t.Fatalf("second sync: %+v %v", job, err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
func TestWaitRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v", err)
	}
}
