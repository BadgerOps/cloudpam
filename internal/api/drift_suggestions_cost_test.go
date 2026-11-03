package api

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"testing"

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
