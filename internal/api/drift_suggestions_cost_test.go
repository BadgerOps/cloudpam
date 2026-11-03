package api

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"runtime"
	"testing"

	"github.com/google/uuid"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

type countingDiscoveryStore struct {
	*storage.MemoryDiscoveryStore
	lists int
}

func (c *countingDiscoveryStore) ListDiscoveredResources(ctx context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, int, error) {
	c.lists++
	return c.MemoryDiscoveryStore.ListDiscoveredResources(ctx, accountID, filters)
}

// detectListings runs detection over unmanaged VPCs spread across three
// accounts and returns how many resource listings the suggestions made.
func detectListings(t *testing.T, unmanaged int) int {
	t.Helper()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	driftStore := storage.NewMemoryDriftStore(ms)
	counted := &countingDiscoveryStore{MemoryDiscoveryStore: ds}

	mux := stdhttp.NewServeMux()
	srv := NewServer(mux, ms, observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard}), nil, nil)
	discSrv := NewDiscoveryServer(srv, counted, discovery.NewSyncService(ds), nil)
	driftSrv := NewDriftServer(srv, discovery.NewDriftDetector(ms, ds, driftStore), driftStore, discSrv)
	mux.HandleFunc("/api/v1/drift/detect", driftSrv.handleDetect)

	ctx := context.Background()
	var accounts []int64
	for n := 0; n < 3; n++ {
		acct, err := ms.CreateAccount(ctx, domain.CreateAccount{Key: fmt.Sprintf("aws:cost-%d", n), Name: "Cost", Provider: "aws"})
		if err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, acct.ID)
	}
	for n := 0; n < unmanaged; n++ {
		addDiscovered(t, ds, accounts[n%len(accounts)], domain.ResourceTypeVPC, fmt.Sprintf("vpc-cost-%d", n), fmt.Sprintf("10.%d.0.0/16", n), nil)
	}

	counted.lists = 0
	items := detectDrift(t, mux)
	if len(items) != unmanaged {
		t.Fatalf("detected %d items, want %d", len(items), unmanaged)
	}
	for _, item := range items {
		assertSuggestion(t, item, domain.DriftActionCreatePool, nil)
	}
	return counted.lists
}

func TestDriftSuggestions_InventoryLoadedOncePerRequest(t *testing.T) {
	few, many := detectListings(t, 3), detectListings(t, 12)
	if many != few {
		t.Fatalf("resource listings grew with drift items: %d for 3 items, %d for 12", few, many)
	}
}

// previewBytes loads one inventory over an account of size VPCs, then returns
// the bytes allocated per single-resource preview once the inventory is warm.
func previewBytes(t *testing.T, size int) uint64 {
	t.Helper()
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	srv := NewServer(stdhttp.NewServeMux(), ms, observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard}), nil, nil)
	d := NewDiscoveryServer(srv, ds, discovery.NewSyncService(ds), nil)

	ctx := context.Background()
	acct, err := ms.CreateAccount(ctx, domain.CreateAccount{Key: fmt.Sprintf("aws:size-%d", size), Name: "Size", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	var vpcs []uuid.UUID
	for n := 0; n < size; n++ {
		vpcs = append(vpcs, addDiscovered(t, ds, acct.ID, domain.ResourceTypeVPC, fmt.Sprintf("vpc-size-%d", n), fmt.Sprintf("10.%d.%d.0/24", n/256, n%256), nil))
	}
	inv, err := d.loadDiscoveryInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview := func(id uuid.UUID) {
		if _, err := d.previewWithInventory(ctx, inv, domain.DiscoveryImportPreviewRequest{AccountID: acct.ID, ResourceIDs: []uuid.UUID{id}}); err != nil {
			t.Fatal(err)
		}
	}
	preview(vpcs[0])

	const runs = 20
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for n := 0; n < runs; n++ {
		preview(vpcs[n])
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / runs
}

// Detection previews each unmanaged resource alone against one inventory, so
// a per-preview cost that grows with the account's inventory is quadratic.
func TestDiscoveryInventory_PreviewCostIndependentOfInventorySize(t *testing.T) {
	small, large := previewBytes(t, 50), previewBytes(t, 2000)
	if large > 4*small {
		t.Fatalf("a warm preview allocated %d bytes over 50 resources and %d over 2000", small, large)
	}
}
