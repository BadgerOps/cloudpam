package storage

import "cloudpam/internal/domain"

// The in-memory stores hand records straight out of their internal maps and
// slices. Copying the struct is not enough: maps and slices inside it stay
// shared, so a caller that mutates (say) a returned Pool's Tags is silently
// editing stored state without holding the store's lock. The SQL-backed stores
// have no such aliasing because they rebuild every record from rows, so these
// helpers exist to give the memory stores the same value semantics.

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	// make + copy rather than append to a nil slice: appending zero elements
	// would collapse an empty-but-present slice back to nil.
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func clonePool(p domain.Pool) domain.Pool {
	p.Tags = cloneStringStringMap(p.Tags)
	return p
}

func cloneAccount(a domain.Account) domain.Account {
	a.Regions = cloneStringSlice(a.Regions)
	return a
}

func cloneDiscoveredResource(r domain.DiscoveredResource) domain.DiscoveredResource {
	r.Metadata = cloneStringStringMap(r.Metadata)
	return r
}

func cloneDiscoveredResources(in []domain.DiscoveredResource) []domain.DiscoveredResource {
	if in == nil {
		return nil
	}
	out := make([]domain.DiscoveredResource, len(in))
	for i, r := range in {
		out[i] = cloneDiscoveredResource(r)
	}
	return out
}

func cloneRecommendation(r domain.Recommendation) domain.Recommendation {
	r.Metadata = cloneStringStringMap(r.Metadata)
	return r
}

func cloneRecommendations(in []domain.Recommendation) []domain.Recommendation {
	if in == nil {
		return nil
	}
	out := make([]domain.Recommendation, len(in))
	for i, r := range in {
		out[i] = cloneRecommendation(r)
	}
	return out
}

func cloneDriftItem(d domain.DriftItem) domain.DriftItem {
	d.Details = cloneStringStringMap(d.Details)
	return d
}

func cloneDriftItems(in []domain.DriftItem) []domain.DriftItem {
	if in == nil {
		return nil
	}
	out := make([]domain.DriftItem, len(in))
	for i, d := range in {
		out[i] = cloneDriftItem(d)
	}
	return out
}

func cloneConversationMessages(in []domain.ConversationMessage) []domain.ConversationMessage {
	if in == nil {
		return nil
	}
	// make + copy rather than append to a nil slice: appending zero elements
	// would hand back nil and turn an empty history into a missing one.
	out := make([]domain.ConversationMessage, len(in))
	copy(out, in)
	return out
}
