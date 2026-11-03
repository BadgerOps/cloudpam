package discovery

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestProcessResourcesCountsExistingResourceBeyondFirstPageAsUpdated(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	svc := NewSyncService(ds)

	now := time.Now().UTC()
	for i := 0; i < 1001; i++ {
		if err := ds.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{
			ID:           uuid.New(),
			AccountID:    42,
			Provider:     "aws",
			Region:       "us-east-1",
			ResourceType: domain.ResourceTypeVPC,
			ResourceID:   "vpc-existing-" + strconv.Itoa(i),
			Name:         "existing",
			CIDR:         "10.0.0.0/16",
			Status:       domain.DiscoveryStatusActive,
			DiscoveredAt: now.Add(time.Duration(i) * time.Second),
			LastSeenAt:   now,
		}); err != nil {
			t.Fatalf("upsert existing resource %d: %v", i, err)
		}
	}

	targetResourceID := "vpc-existing-0"
	created, updated, _, err := svc.ProcessResources(ctx, 42, []domain.DiscoveredResource{{
		ID:           uuid.New(),
		AccountID:    42,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   targetResourceID,
		Name:         "updated",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: now,
		LastSeenAt:   now.Add(time.Minute),
	}}, now)
	if err != nil {
		t.Fatalf("ProcessResources() error: %v", err)
	}
	if created != 0 || updated != 1 {
		t.Fatalf("created=%d updated=%d, want created=0 updated=1", created, updated)
	}
}

func TestSyncFailsWhenProcessingResourcesFails(t *testing.T) {
	ctx := context.Background()
	ms := storage.NewMemoryStore()
	ds := &markStaleFailStore{
		MemoryDiscoveryStore: storage.NewMemoryDiscoveryStore(ms),
		err:                  errors.New("stale update failed"),
	}
	svc := NewSyncService(ds)
	svc.RegisterCollector(staticCollector{resources: []domain.DiscoveredResource{{
		ID:           uuid.New(),
		AccountID:    42,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-1",
		Name:         "prod",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}}})

	job, err := svc.Sync(ctx, domain.Account{ID: 42, Provider: "aws"})
	if err == nil {
		t.Fatal("Sync() error = nil, want processing error")
	}
	if !strings.Contains(err.Error(), "process resources") {
		t.Fatalf("Sync() error = %q, want process resources context", err.Error())
	}
	if job == nil {
		t.Fatal("Sync() job = nil")
	}
	if job.Status != domain.SyncJobStatusFailed {
		t.Fatalf("job.Status = %q, want %q", job.Status, domain.SyncJobStatusFailed)
	}
	if job.ErrorMessage == "" {
		t.Fatal("job.ErrorMessage is empty")
	}
}

type staticCollector struct {
	resources []domain.DiscoveredResource
	err       error
}

func (c staticCollector) Provider() string { return "aws" }

func (c staticCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	return c.resources, c.err
}

type markStaleFailStore struct {
	*storage.MemoryDiscoveryStore
	err error
}

func (s *markStaleFailStore) MarkStaleResources(context.Context, int64, time.Time) (int, error) {
	return 0, s.err
}

func TestSyncClipsStoredErrorMessage(t *testing.T) {
	ctx := context.Background()
	ds := storage.NewMemoryDiscoveryStore(storage.NewMemoryStore())
	svc := NewSyncService(ds)
	svc.RegisterCollector(staticCollector{err: errors.New(strings.Repeat("y", 1<<20))})

	job, err := svc.Sync(ctx, domain.Account{ID: 42, Provider: "aws"})
	if err == nil {
		t.Fatal("Sync() error = nil, want discovery error")
	}
	stored, err := ds.GetSyncJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSyncJob: %v", err)
	}
	if len(stored.ErrorMessage) > maxSyncErrorMessage+len("... (truncated)") {
		t.Fatalf("stored ErrorMessage length = %d, want at most %d", len(stored.ErrorMessage), maxSyncErrorMessage+len("... (truncated)"))
	}
	if !strings.HasSuffix(stored.ErrorMessage, "... (truncated)") {
		t.Fatalf("stored ErrorMessage %q lacks truncation marker", stored.ErrorMessage[len(stored.ErrorMessage)-40:])
	}
}

func TestSyncErrorMessageKeepsPrefixAndRuneBoundary(t *testing.T) {
	msg := syncErrorMessage("credential: ", errors.New(strings.Repeat("é", maxSyncErrorMessage)))
	if !strings.HasPrefix(msg, "credential: ") {
		t.Fatalf("prefix lost: %q", msg[:20])
	}
	if !utf8.ValidString(msg) {
		t.Fatal("truncated message is not valid UTF-8")
	}
	short := syncErrorMessage("", errors.New("boom"))
	if short != "boom" {
		t.Fatalf("short message = %q, want boom", short)
	}
}
