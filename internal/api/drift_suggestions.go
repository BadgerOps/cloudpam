package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// suggestReconciliation fills the suggestion fields of every open item from
// current pools and discovered resources. Unmanaged resources are classified
// by the discovery import preview, so a suggestion is exactly what the link or
// import apply endpoint will do with that resource.
func (ds *DriftServer) suggestReconciliation(ctx context.Context, items []domain.DriftItem) error {
	pools, err := ds.srv.store.ListPools(ctx)
	if err != nil {
		return err
	}
	poolsByID := make(map[int64]domain.Pool, len(pools))
	for _, p := range pools {
		poolsByID[p.ID] = p
	}

	unmanaged := map[int64][]int{}
	for i := range items {
		item := &items[i]
		if item.Status != domain.DriftStatusOpen {
			continue
		}
		switch item.Type {
		case domain.DriftTypeUnmanaged:
			if item.ResourceID != nil {
				unmanaged[item.AccountID] = append(unmanaged[item.AccountID], i)
				continue
			}
			suggestReview(item, "the drift item has no discovered resource")
		case domain.DriftTypeCIDRMismatch:
			suggestRelink(item, pools)
		case domain.DriftTypeOrphanedPool:
			suggestDeprecate(item, poolsByID)
		default:
			suggestReview(item, "no automatic reconciliation exists for this drift type")
		}
	}

	for accountID, indexes := range unmanaged {
		ids := make([]uuid.UUID, len(indexes))
		for n, i := range indexes {
			ids[n] = *items[i].ResourceID
		}
		preview, err := ds.discovery.previewDiscoveryImport(ctx, domain.DiscoveryImportPreviewRequest{AccountID: accountID, ResourceIDs: ids})
		if err != nil {
			if isDiscoveryImportClientError(err) {
				for _, i := range indexes {
					suggestReview(&items[i], err.Error())
				}
				continue
			}
			return err
		}
		byResource := make(map[uuid.UUID]domain.DiscoveryImportPreviewItem, len(preview.Items))
		for _, p := range preview.Items {
			byResource[p.ResourceID] = p
		}
		for _, i := range indexes {
			suggestFromPreview(&items[i], byResource[*items[i].ResourceID])
		}
	}
	return nil
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
// pool that now matches it, under the same account rule as the link endpoint.
func suggestRelink(item *domain.DriftItem, pools []domain.Pool) {
	if item.ResourceID == nil {
		suggestReview(item, "the drift item has no discovered resource")
		return
	}
	for _, p := range pools {
		if item.PoolID != nil && p.ID == *item.PoolID {
			continue
		}
		if p.AccountID != nil && *p.AccountID != item.AccountID {
			continue
		}
		if cidrEqual(p.CIDR, item.ResourceCIDR) {
			id := p.ID
			item.SuggestedAction = domain.DriftActionLinkPool
			item.SuggestedPoolID = &id
			item.SuggestionReason = fmt.Sprintf("pool %d has the resource's current CIDR", id)
			return
		}
	}
	suggestReview(item, "no managed pool has the resource's current CIDR; update the pool or relink by hand")
}

func suggestDeprecate(item *domain.DriftItem, poolsByID map[int64]domain.Pool) {
	if item.PoolID == nil {
		suggestReview(item, "the drift item has no pool")
		return
	}
	pool, ok := poolsByID[*item.PoolID]
	switch {
	case !ok:
		suggestReview(item, "the pool no longer exists")
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
