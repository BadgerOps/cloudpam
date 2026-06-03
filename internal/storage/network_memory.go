package storage

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloudpam/internal/domain"
)

// MemoryNetworkStore stores managed network objects and relationships in memory.
type MemoryNetworkStore struct {
	store         *MemoryStore
	objects       map[int64]domain.NetworkObject
	relationships map[string]domain.NetworkRelationship
	nextObjectID  int64
}

func NewMemoryNetworkStore(store *MemoryStore) *MemoryNetworkStore {
	if store == nil {
		store = NewMemoryStore()
	}
	return &MemoryNetworkStore{
		store:         store,
		objects:       make(map[int64]domain.NetworkObject),
		relationships: make(map[string]domain.NetworkRelationship),
		nextObjectID:  1,
	}
}

func (m *MemoryNetworkStore) ListNetworkObjects(_ context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	out := make([]domain.NetworkObject, 0, len(m.objects))
	for _, obj := range m.objects {
		if filters.AccountID > 0 && obj.AccountID != filters.AccountID {
			continue
		}
		if filters.Provider != "" && obj.Provider != filters.Provider {
			continue
		}
		if filters.Region != "" && obj.Region != filters.Region {
			continue
		}
		if filters.ObjectType != "" && string(obj.ObjectType) != filters.ObjectType {
			continue
		}
		if filters.State != "" && string(obj.State) != filters.State {
			continue
		}
		if filters.PoolID > 0 && (obj.PoolID == nil || *obj.PoolID != filters.PoolID) {
			continue
		}
		if filters.SourceDiscoveredID != "" && (obj.SourceDiscoveredID == nil || obj.SourceDiscoveredID.String() != filters.SourceDiscoveredID) {
			continue
		}
		if filters.Query != "" {
			q := strings.ToLower(filters.Query)
			haystack := strings.ToLower(strings.Join([]string{obj.Name, obj.CIDR, obj.IPAddress, obj.ProviderResourceID}, " "))
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		out = append(out, cloneNetworkObject(obj))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AccountID == out[j].AccountID {
			if out[i].Region == out[j].Region {
				return out[i].Name < out[j].Name
			}
			return out[i].Region < out[j].Region
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out, nil
}

func (m *MemoryNetworkStore) GetNetworkObject(_ context.Context, id int64) (domain.NetworkObject, bool, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	obj, ok := m.objects[id]
	return cloneNetworkObject(obj), ok, nil
}

func (m *MemoryNetworkStore) CreateNetworkObject(_ context.Context, in domain.CreateNetworkObject) (domain.NetworkObject, error) {
	if in.AccountID < 1 {
		return domain.NetworkObject{}, fmt.Errorf("account_id is required: %w", ErrValidation)
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.NetworkObject{}, fmt.Errorf("name is required: %w", ErrValidation)
	}
	objectType := in.ObjectType
	if objectType == "" {
		objectType = domain.NetworkObjectTypeOther
	}
	state := in.State
	if state == "" {
		state = domain.NetworkObjectStateManaged
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	now := time.Now().UTC()
	obj := domain.NetworkObject{
		ID:                 m.nextObjectID,
		ObjectType:         objectType,
		Provider:           in.Provider,
		AccountID:          in.AccountID,
		Region:             in.Region,
		Name:               in.Name,
		CIDR:               in.CIDR,
		IPAddress:          in.IPAddress,
		ProviderResourceID: in.ProviderResourceID,
		ParentObjectID:     in.ParentObjectID,
		PoolID:             in.PoolID,
		SourceDiscoveredID: in.SourceDiscoveredID,
		State:              state,
		Metadata:           cloneStringMap(in.Metadata),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	m.nextObjectID++
	m.objects[obj.ID] = obj
	return cloneNetworkObject(obj), nil
}

func (m *MemoryNetworkStore) UpdateNetworkObject(_ context.Context, id int64, update domain.UpdateNetworkObject) (domain.NetworkObject, bool, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	obj, ok := m.objects[id]
	if !ok {
		return domain.NetworkObject{}, false, nil
	}
	if update.ObjectType != nil {
		obj.ObjectType = *update.ObjectType
	}
	if update.Provider != nil {
		obj.Provider = *update.Provider
	}
	if update.AccountID != nil {
		obj.AccountID = *update.AccountID
	}
	if update.Region != nil {
		obj.Region = *update.Region
	}
	if update.Name != nil {
		obj.Name = *update.Name
	}
	if update.CIDR != nil {
		obj.CIDR = *update.CIDR
	}
	if update.IPAddress != nil {
		obj.IPAddress = *update.IPAddress
	}
	if update.ProviderResourceID != nil {
		obj.ProviderResourceID = *update.ProviderResourceID
	}
	if update.ParentObjectID != nil {
		obj.ParentObjectID = update.ParentObjectID
	}
	if update.PoolID != nil {
		obj.PoolID = update.PoolID
	}
	if update.SourceDiscoveredID != nil {
		obj.SourceDiscoveredID = update.SourceDiscoveredID
	}
	if update.State != nil {
		obj.State = *update.State
	}
	if update.Metadata != nil {
		obj.Metadata = cloneStringMap(*update.Metadata)
	}
	obj.UpdatedAt = time.Now().UTC()
	m.objects[id] = obj
	return cloneNetworkObject(obj), true, nil
}

func (m *MemoryNetworkStore) ListNetworkRelationships(_ context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	out := make([]domain.NetworkRelationship, 0, len(m.relationships))
	ids := map[string]struct{}{}
	for _, id := range filters.IDs {
		ids[id] = struct{}{}
	}
	for _, rel := range m.relationships {
		if len(ids) > 0 {
			if _, ok := ids[rel.ID]; !ok {
				continue
			}
		}
		if filters.Type != "" && string(rel.Type) != filters.Type {
			continue
		}
		if filters.SourceKind != "" && rel.SourceKind != filters.SourceKind {
			continue
		}
		if filters.SourceID != "" && rel.SourceID != filters.SourceID {
			continue
		}
		if filters.TargetKind != "" && rel.TargetKind != filters.TargetKind {
			continue
		}
		if filters.TargetID != "" && rel.TargetID != filters.TargetID {
			continue
		}
		if filters.EntityKind != "" && filters.EntityID != "" &&
			!((rel.SourceKind == filters.EntityKind && rel.SourceID == filters.EntityID) ||
				(rel.TargetKind == filters.EntityKind && rel.TargetID == filters.EntityID)) {
			continue
		}
		if filters.ResolutionState != "" && rel.ResolutionState != filters.ResolutionState {
			continue
		}
		out = append(out, cloneNetworkRelationship(rel))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryNetworkStore) UpsertNetworkRelationship(_ context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error) {
	if in.Type == "" || in.SourceKind == "" || in.SourceID == "" || in.TargetKind == "" || in.TargetID == "" {
		return domain.NetworkRelationship{}, fmt.Errorf("relationship type, source, and target are required: %w", ErrValidation)
	}
	id := in.ID
	if id == "" {
		id = networkRelationshipID(in)
	}
	confidence := in.Confidence
	if confidence == 0 {
		confidence = 1
	}
	state := in.ResolutionState
	if state == "" {
		state = "open"
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	now := time.Now().UTC()
	rel := domain.NetworkRelationship{
		ID:              id,
		Type:            in.Type,
		SourceKind:      in.SourceKind,
		SourceID:        in.SourceID,
		TargetKind:      in.TargetKind,
		TargetID:        in.TargetID,
		Confidence:      confidence,
		Reason:          in.Reason,
		Evidence:        append([]string(nil), in.Evidence...),
		ResolutionState: state,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if existing, ok := m.relationships[id]; ok {
		rel.CreatedAt = existing.CreatedAt
		if in.ResolutionState == "" {
			rel.ResolutionState = existing.ResolutionState
		}
	}
	m.relationships[id] = rel
	return cloneNetworkRelationship(rel), nil
}

func (m *MemoryNetworkStore) UpdateNetworkRelationshipState(_ context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error) {
	if strings.TrimSpace(state) == "" {
		return domain.NetworkRelationship{}, false, fmt.Errorf("resolution_state is required: %w", ErrValidation)
	}
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	rel, ok := m.relationships[id]
	if !ok {
		return domain.NetworkRelationship{}, false, nil
	}
	rel.ResolutionState = state
	if reason != "" {
		rel.Reason = reason
	}
	rel.UpdatedAt = time.Now().UTC()
	m.relationships[id] = rel
	return cloneNetworkRelationship(rel), true, nil
}

func networkRelationshipID(in domain.CreateNetworkRelationship) string {
	sum := sha1.Sum([]byte(strings.Join([]string{string(in.Type), in.SourceKind, in.SourceID, in.TargetKind, in.TargetID}, "|")))
	return "rel-" + hex.EncodeToString(sum[:])[:20]
}

func cloneNetworkObject(obj domain.NetworkObject) domain.NetworkObject {
	obj.Metadata = cloneStringMap(obj.Metadata)
	return obj
}

func cloneNetworkRelationship(rel domain.NetworkRelationship) domain.NetworkRelationship {
	rel.Evidence = append([]string(nil), rel.Evidence...)
	return rel
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
