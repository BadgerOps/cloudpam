package api

import (
	"context"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// discoveryInventory is the pool and discovered-resource state an import
// preview reads. One request loads it once, so previewing many resources does
// not rescan the whole inventory per resource. It is never reused across
// requests, because writes would leave it stale.
type discoveryInventory struct {
	d            *DiscoveryServer
	pools        []domain.Pool
	poolsByID    map[int64]domain.Pool
	accountIDs   []int64
	accounts     map[int64]bool
	resources    map[int64][]domain.DiscoveredResource
	activeByCIDR map[string][]domain.DiscoveredResource
}

func (d *DiscoveryServer) loadDiscoveryInventory(ctx context.Context) (*discoveryInventory, error) {
	pools, err := d.srv.store.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := d.srv.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	inv := &discoveryInventory{
		d:         d,
		pools:     pools,
		poolsByID: make(map[int64]domain.Pool, len(pools)),
		accounts:  make(map[int64]bool, len(accounts)),
		resources: map[int64][]domain.DiscoveredResource{},
	}
	for _, pool := range pools {
		inv.poolsByID[pool.ID] = pool
	}
	for _, account := range accounts {
		inv.accountIDs = append(inv.accountIDs, account.ID)
		inv.accounts[account.ID] = true
	}
	return inv, nil
}

// accountResources returns every discovered resource of the account, in any
// status.
func (inv *discoveryInventory) accountResources(ctx context.Context, accountID int64) ([]domain.DiscoveredResource, error) {
	if res, ok := inv.resources[accountID]; ok {
		return res, nil
	}
	res, err := inv.d.listAllDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{})
	if err != nil {
		return nil, err
	}
	inv.resources[accountID] = res
	return res, nil
}

// duplicateCIDRs maps each CIDR of the selected resources to the active
// resources of other accounts that share it.
func (inv *discoveryInventory) duplicateCIDRs(ctx context.Context, accountID int64, selected []domain.DiscoveredResource) (map[string][]uuid.UUID, error) {
	out := map[string][]uuid.UUID{}
	for _, res := range selected {
		if res.CIDR == "" {
			continue
		}
		if _, done := out[res.CIDR]; done {
			continue
		}
		if inv.activeByCIDR == nil {
			if err := inv.indexActiveByCIDR(ctx); err != nil {
				return nil, err
			}
		}
		ids := []uuid.UUID{}
		for _, other := range inv.activeByCIDR[res.CIDR] {
			if other.AccountID != accountID {
				ids = append(ids, other.ID)
			}
		}
		out[res.CIDR] = ids
	}
	return out, nil
}

func (inv *discoveryInventory) indexActiveByCIDR(ctx context.Context) error {
	index := map[string][]domain.DiscoveredResource{}
	for _, accountID := range inv.accountIDs {
		resources, err := inv.d.listAllDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{Status: string(domain.DiscoveryStatusActive)})
		if err != nil {
			return err
		}
		for _, res := range resources {
			if res.CIDR != "" {
				index[res.CIDR] = append(index[res.CIDR], res)
			}
		}
	}
	inv.activeByCIDR = index
	return nil
}
