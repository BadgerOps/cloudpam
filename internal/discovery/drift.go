package discovery

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// DriftDetector compares discovered cloud resources against managed IPAM pools.
type DriftDetector struct {
	store      storage.Store
	discStore  storage.DiscoveryStore
	driftStore storage.DriftStore
}

// NewDriftDetector creates a new DriftDetector.
func NewDriftDetector(store storage.Store, discStore storage.DiscoveryStore, driftStore storage.DriftStore) *DriftDetector {
	return &DriftDetector{store: store, discStore: discStore, driftStore: driftStore}
}

// Detect runs drift detection across the specified accounts (or all accounts if empty).
func (d *DriftDetector) Detect(ctx context.Context, req domain.RunDriftDetectionRequest) (*domain.RunDriftDetectionResponse, error) {
	now := time.Now().UTC()

	accounts, err := d.store.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	// Filter to requested accounts if specified.
	requestedIDs := make(map[int64]bool, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		requestedIDs[id] = true
	}

	var targetAccounts []domain.Account
	for _, a := range accounts {
		if len(requestedIDs) == 0 || requestedIDs[a.ID] {
			targetAccounts = append(targetAccounts, a)
		}
	}

	pools, err := d.store.ListPools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	// Index pools by account ID for quick lookup.
	poolsByAccount := make(map[int64][]domain.Pool)
	for _, p := range pools {
		if p.AccountID != nil {
			poolsByAccount[*p.AccountID] = append(poolsByAccount[*p.AccountID], p)
		}
	}

	var allItems []domain.DriftItem
	totalResources := 0
	totalPools := len(pools)

	for _, acct := range targetAccounts {
		// Clear previous open drift items for this account before re-detection.
		if err := d.driftStore.DeleteOpenForAccount(ctx, acct.ID); err != nil {
			return nil, fmt.Errorf("clear open drifts for account %d: %w", acct.ID, err)
		}

		resources, _, err := d.discStore.ListDiscoveredResources(ctx, acct.ID, domain.DiscoveryFilters{
			Status:   string(domain.DiscoveryStatusActive),
			Page:     1,
			PageSize: 10000,
		})
		if err != nil {
			return nil, fmt.Errorf("list resources for account %d: %w", acct.ID, err)
		}
		totalResources += len(resources)

		accountPools := poolsByAccount[acct.ID]

		// Pass 1: Check each active resource for drift.
		for i := range resources {
			res := &resources[i]
			if res.CIDR == "" {
				continue
			}

			if res.PoolID == nil {
				// Unmanaged: cloud resource with no linked pool.
				severity := domain.DriftSeverityWarning
				if res.ResourceType == domain.ResourceTypeNetworkInterface || res.ResourceType == domain.ResourceTypeElasticIP {
					severity = domain.DriftSeverityInfo
				}
				item := domain.DriftItem{
					ID:           uuid.New().String(),
					AccountID:    acct.ID,
					ResourceID:   &res.ID,
					Type:         domain.DriftTypeUnmanaged,
					Severity:     severity,
					Status:       domain.DriftStatusOpen,
					Title:        fmt.Sprintf("Unmanaged %s: %s", res.ResourceType, res.Name),
					Description:  fmt.Sprintf("Cloud resource %s (%s) in %s has no linked IPAM pool", res.ResourceID, res.CIDR, res.Region),
					ResourceCIDR: res.CIDR,
					Details:      map[string]string{"resource_type": string(res.ResourceType), "region": res.Region, "provider": res.Provider},
					DetectedAt:   now,
					UpdatedAt:    now,
				}
				if err := d.driftStore.CreateDriftItem(ctx, item); err != nil {
					return nil, fmt.Errorf("create drift item: %w", err)
				}
				allItems = append(allItems, item)
			} else {
				// Resource is linked — check for CIDR and name mismatches.
				pool, found, err := d.store.GetPool(ctx, *res.PoolID)
				if err != nil {
					return nil, fmt.Errorf("get pool %d: %w", *res.PoolID, err)
				}
				if !found {
					continue
				}

				// CIDR mismatch
				if !cidrsEqual(res.CIDR, pool.CIDR) {
					item := domain.DriftItem{
						ID:           uuid.New().String(),
						AccountID:    acct.ID,
						ResourceID:   &res.ID,
						PoolID:       &pool.ID,
						Type:         domain.DriftTypeCIDRMismatch,
						Severity:     domain.DriftSeverityCritical,
						Status:       domain.DriftStatusOpen,
						Title:        fmt.Sprintf("CIDR mismatch: %s vs %s", res.CIDR, pool.CIDR),
						Description:  fmt.Sprintf("Resource %s has CIDR %s but linked pool %q has CIDR %s", res.ResourceID, res.CIDR, pool.Name, pool.CIDR),
						ResourceCIDR: res.CIDR,
						PoolCIDR:     pool.CIDR,
						Details:      map[string]string{"resource_id": res.ResourceID, "pool_name": pool.Name},
						DetectedAt:   now,
						UpdatedAt:    now,
					}
					if err := d.driftStore.CreateDriftItem(ctx, item); err != nil {
						return nil, fmt.Errorf("create drift item: %w", err)
					}
					allItems = append(allItems, item)
				}
			}
		}

		// Pass 2: Check for orphaned pools (pools with source=discovered whose resource is gone).
		for i := range accountPools {
			p := &accountPools[i]
			if p.Source != domain.PoolSourceDiscovered {
				continue
			}
			// Check if any active resource is linked to this pool.
			linked := false
			for j := range resources {
				if resources[j].PoolID != nil && *resources[j].PoolID == p.ID {
					linked = true
					break
				}
			}
			if !linked {
				item := domain.DriftItem{
					ID:          uuid.New().String(),
					AccountID:   acct.ID,
					PoolID:      &p.ID,
					Type:        domain.DriftTypeOrphanedPool,
					Severity:    domain.DriftSeverityWarning,
					Status:      domain.DriftStatusOpen,
					Title:       fmt.Sprintf("Orphaned pool: %s", p.Name),
					Description: fmt.Sprintf("Pool %q (%s) was created from discovery but has no active linked cloud resource", p.Name, p.CIDR),
					PoolCIDR:    p.CIDR,
					Details:     map[string]string{"pool_name": p.Name},
					DetectedAt:  now,
					UpdatedAt:   now,
				}
				if err := d.driftStore.CreateDriftItem(ctx, item); err != nil {
					return nil, fmt.Errorf("create drift item: %w", err)
				}
				allItems = append(allItems, item)
			}
		}
	}

	summary := buildSummary(allItems, len(targetAccounts), totalResources, totalPools)

	return &domain.RunDriftDetectionResponse{
		Items:   allItems,
		Total:   len(allItems),
		Summary: summary,
	}, nil
}

func buildSummary(items []domain.DriftItem, accountsScanned, resourcesScanned, poolsScanned int) domain.DriftSummary {
	bySeverity := map[string]int{}
	byType := map[string]int{}
	for _, item := range items {
		bySeverity[string(item.Severity)]++
		byType[string(item.Type)]++
	}
	return domain.DriftSummary{
		TotalDrifts:      len(items),
		BySeverity:       bySeverity,
		ByType:           byType,
		AccountsScanned:  accountsScanned,
		ResourcesScanned: resourcesScanned,
		PoolsScanned:     poolsScanned,
	}
}

// cidrsEqual normalizes and compares two CIDR strings.
func cidrsEqual(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return pa.Masked() == pb.Masked()
}
