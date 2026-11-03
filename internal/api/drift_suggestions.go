package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// suggestReconciliation fills the suggestion fields of every open item from
// current pools and discovered resources, never from the snapshot stored at
// detection time. Each unmanaged resource is classified by the discovery import
// preview with only that resource selected, so a suggestion is exactly what the
// link or single-resource import apply will do with it.
func (ds *DriftServer) suggestReconciliation(ctx context.Context, items []domain.DriftItem) error {
	pools, err := ds.srv.store.ListPools(ctx)
	if err != nil {
		return err
	}
	poolsByID := make(map[int64]domain.Pool, len(pools))
	for _, p := range pools {
		poolsByID[p.ID] = p
	}
	activeLinks := map[int64]map[int64]bool{}

	for i := range items {
		item := &items[i]
		if item.Status != domain.DriftStatusOpen {
			continue
		}
		switch item.Type {
		case domain.DriftTypeUnmanaged:
			if err := ds.suggestImport(ctx, item); err != nil {
				return err
			}
		case domain.DriftTypeCIDRMismatch:
			if err := ds.suggestRelink(ctx, item, pools); err != nil {
				return err
			}
		case domain.DriftTypeOrphanedPool:
			linked, ok := activeLinks[item.AccountID]
			if !ok {
				if linked, err = ds.activePoolLinks(ctx, item.AccountID); err != nil {
					return err
				}
				activeLinks[item.AccountID] = linked
			}
			suggestDeprecate(item, poolsByID, linked)
		default:
			suggestReview(item, "no automatic reconciliation exists for this drift type")
		}
	}
	return nil
}

func (ds *DriftServer) suggestImport(ctx context.Context, item *domain.DriftItem) error {
	if item.ResourceID == nil {
		suggestReview(item, "the drift item has no discovered resource")
		return nil
	}
	preview, err := ds.discovery.previewDiscoveryImport(ctx, domain.DiscoveryImportPreviewRequest{AccountID: item.AccountID, ResourceIDs: []uuid.UUID{*item.ResourceID}})
	if err != nil {
		if isDiscoveryImportClientError(err) {
			suggestReview(item, err.Error())
			return nil
		}
		return err
	}
	for _, p := range preview.Items {
		if p.ResourceID == *item.ResourceID {
			suggestFromPreview(item, p)
			return nil
		}
	}
	suggestReview(item, "the discovered resource no longer exists; rerun detection")
	return nil
}

// activePoolLinks returns the pools an active discovered resource of the
// account links to, the same test detection uses to call a pool orphaned.
func (ds *DriftServer) activePoolLinks(ctx context.Context, accountID int64) (map[int64]bool, error) {
	resources, err := ds.discovery.listAllDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{Status: string(domain.DiscoveryStatusActive)})
	if err != nil {
		return nil, err
	}
	linked := map[int64]bool{}
	for _, res := range resources {
		if res.PoolID != nil {
			linked[*res.PoolID] = true
		}
	}
	return linked, nil
}

func suggestFromPreview(item *domain.DriftItem, p domain.DiscoveryImportPreviewItem) {
	switch {
	case p.Status == "importable" && p.ProposedAction == "link_pool":
		item.SuggestedAction = domain.DriftActionLinkPool
		item.SuggestedPoolID = p.ProposedPoolID
		item.SuggestionReason = fmt.Sprintf("pool %d has exactly this CIDR", *p.ProposedPoolID)
	case p.Status == "importable":
		item.SuggestedAction = domain.DriftActionCreatePool
		item.SuggestedPoolID = p.ProposedParentPoolID
		if p.ProposedParentPoolID != nil {
			item.SuggestionReason = fmt.Sprintf("create a pool under containing pool %d", *p.ProposedParentPoolID)
		} else {
			item.SuggestionReason = "create a top-level pool; no managed pool contains this CIDR"
		}
	case p.Status == "linked_only":
		suggestReview(item, "network interfaces and elastic IPs are tracked as network objects, not pools")
	case p.Status == "":
		suggestReview(item, "the discovered resource was not classified")
	default:
		reason := "import is blocked: " + strings.Join(p.Issues, ", ")
		if len(p.ConflictPoolIDs) > 0 {
			ids := make([]string, len(p.ConflictPoolIDs))
			for n, id := range p.ConflictPoolIDs {
				ids[n] = strconv.FormatInt(id, 10)
			}
			reason = "the CIDR overlaps managed pools " + strings.Join(ids, ", ")
		}
		suggestReview(item, reason)
	}
}

// suggestRelink proposes moving a linked resource whose CIDR changed onto the
// pool that matches its current CIDR, under the same account rule as the link
// endpoint. A resource whose link or CIDR changed since detection gets a review.
func (ds *DriftServer) suggestRelink(ctx context.Context, item *domain.DriftItem, pools []domain.Pool) error {
	if item.ResourceID == nil {
		suggestReview(item, "the drift item has no discovered resource")
		return nil
	}
	res, err := ds.discovery.store.GetDiscoveredResource(ctx, *item.ResourceID)
	if errors.Is(err, storage.ErrNotFound) {
		suggestReview(item, "the discovered resource no longer exists; rerun detection")
		return nil
	}
	if err != nil {
		return err
	}
	var linked *domain.Pool
	for i := range pools {
		if res.PoolID != nil && pools[i].ID == *res.PoolID {
			linked = &pools[i]
		}
	}
	switch {
	case res.Status != domain.DiscoveryStatusActive:
		suggestReview(item, "the discovered resource is no longer active; rerun detection")
		return nil
	case res.PoolID == nil || item.PoolID == nil || *res.PoolID != *item.PoolID || linked == nil:
		suggestReview(item, "the resource's pool link changed since detection; rerun detection")
		return nil
	case cidrEqual(linked.CIDR, res.CIDR):
		suggestReview(item, "the resource's CIDR now matches its linked pool; rerun detection")
		return nil
	}
	for _, p := range pools {
		if p.ID == linked.ID {
			continue
		}
		if p.AccountID != nil && *p.AccountID != res.AccountID {
			continue
		}
		if cidrEqual(p.CIDR, res.CIDR) {
			id := p.ID
			item.SuggestedAction = domain.DriftActionLinkPool
			item.SuggestedPoolID = &id
			item.SuggestionReason = fmt.Sprintf("pool %d has the resource's current CIDR", id)
			return nil
		}
	}
	suggestReview(item, "no managed pool has the resource's current CIDR; update the pool or relink by hand")
	return nil
}

func suggestDeprecate(item *domain.DriftItem, poolsByID map[int64]domain.Pool, activeLinks map[int64]bool) {
	if item.PoolID == nil {
		suggestReview(item, "the drift item has no pool")
		return
	}
	pool, ok := poolsByID[*item.PoolID]
	switch {
	case !ok:
		suggestReview(item, "the pool no longer exists")
	case activeLinks[pool.ID]:
		suggestReview(item, "an active discovered resource links to this pool again; rerun detection")
	case pool.Status == domain.PoolStatusDeprecated:
		suggestReview(item, "the pool is already deprecated; delete it or resolve this item")
	default:
		id := pool.ID
		item.SuggestedAction = domain.DriftActionDeprecatePool
		item.SuggestedPoolID = &id
		item.SuggestionReason = "the discovered resource behind this pool is no longer active"
	}
}

func suggestReview(item *domain.DriftItem, reason string) {
	item.SuggestedAction = domain.DriftActionReview
	item.SuggestedPoolID = nil
	item.SuggestionReason = reason
}
