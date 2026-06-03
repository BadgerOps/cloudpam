//go:build postgres

package postgres

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.NetworkStore = (*Store)(nil)

func (s *Store) ListNetworkObjects(ctx context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error) {
	where := []string{"organization_id = $1"}
	args := []any{s.orgID}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if filters.AccountID > 0 {
		where = append(where, "account_id = "+addArg(filters.AccountID))
	}
	if filters.Provider != "" {
		where = append(where, "provider = "+addArg(filters.Provider))
	}
	if filters.Region != "" {
		where = append(where, "region = "+addArg(filters.Region))
	}
	if filters.ObjectType != "" {
		where = append(where, "object_type = "+addArg(filters.ObjectType))
	}
	if filters.State != "" {
		where = append(where, "state = "+addArg(filters.State))
	}
	if filters.Query != "" {
		q := "%" + strings.ToLower(filters.Query) + "%"
		where = append(where, fmt.Sprintf("(LOWER(name) LIKE %s OR LOWER(cidr) LIKE %s OR LOWER(ip_address) LIKE %s OR LOWER(provider_resource_id) LIKE %s)", addArg(q), addArg(q), addArg(q), addArg(q)))
	}
	rows, err := s.pool.Query(ctx, `SELECT id, object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at
		FROM network_objects WHERE `+strings.Join(where, " AND ")+` ORDER BY account_id ASC, region ASC, name ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.NetworkObject
	for rows.Next() {
		obj, err := scanPostgresNetworkObject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	if out == nil {
		out = []domain.NetworkObject{}
	}
	return out, rows.Err()
}

func (s *Store) GetNetworkObject(ctx context.Context, id int64) (domain.NetworkObject, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at
		FROM network_objects WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	obj, err := scanPostgresNetworkObject(row)
	if err != nil {
		if err == storage.ErrNotFound {
			return domain.NetworkObject{}, false, nil
		}
		return domain.NetworkObject{}, false, err
	}
	return obj, true, nil
}

func (s *Store) CreateNetworkObject(ctx context.Context, in domain.CreateNetworkObject) (domain.NetworkObject, error) {
	if in.AccountID < 1 {
		return domain.NetworkObject{}, fmt.Errorf("account_id is required: %w", storage.ErrValidation)
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.NetworkObject{}, fmt.Errorf("name is required: %w", storage.ErrValidation)
	}
	objectType := in.ObjectType
	if objectType == "" {
		objectType = domain.NetworkObjectTypeOther
	}
	state := in.State
	if state == "" {
		state = domain.NetworkObjectStateManaged
	}
	metadata, _ := json.Marshal(in.Metadata)
	if in.Metadata == nil {
		metadata = []byte("{}")
	}
	now := time.Now().UTC()
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO network_objects
		(organization_id, object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16)
		RETURNING id`,
		s.orgID, string(objectType), in.Provider, in.AccountID, in.Region, in.Name, in.CIDR, in.IPAddress, in.ProviderResourceID,
		in.ParentObjectID, in.PoolID, in.SourceDiscoveredID, string(state), string(metadata), now, now).Scan(&id)
	if err != nil {
		return domain.NetworkObject{}, err
	}
	obj, _, err := s.GetNetworkObject(ctx, id)
	return obj, err
}

func (s *Store) UpdateNetworkObject(ctx context.Context, id int64, update domain.UpdateNetworkObject) (domain.NetworkObject, bool, error) {
	obj, found, err := s.GetNetworkObject(ctx, id)
	if err != nil || !found {
		return domain.NetworkObject{}, found, err
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
		obj.Metadata = *update.Metadata
	}
	metadata, _ := json.Marshal(obj.Metadata)
	if obj.Metadata == nil {
		metadata = []byte("{}")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE network_objects SET
		object_type = $1, provider = $2, account_id = $3, region = $4, name = $5, cidr = $6, ip_address = $7, provider_resource_id = $8,
		parent_object_id = $9, pool_id = $10, source_discovered_id = $11, state = $12, metadata = $13::jsonb, updated_at = $14
		WHERE id = $15 AND organization_id = $16`,
		string(obj.ObjectType), obj.Provider, obj.AccountID, obj.Region, obj.Name, obj.CIDR, obj.IPAddress, obj.ProviderResourceID,
		obj.ParentObjectID, obj.PoolID, obj.SourceDiscoveredID, string(obj.State), string(metadata), time.Now().UTC(), id, s.orgID)
	if err != nil {
		return domain.NetworkObject{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.NetworkObject{}, false, nil
	}
	obj, _, err = s.GetNetworkObject(ctx, id)
	return obj, true, err
}

func (s *Store) ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error) {
	where := []string{"organization_id = $1"}
	args := []any{s.orgID}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if filters.Type != "" {
		where = append(where, "type = "+addArg(filters.Type))
	}
	if filters.SourceKind != "" {
		where = append(where, "source_kind = "+addArg(filters.SourceKind))
	}
	if filters.SourceID != "" {
		where = append(where, "source_id = "+addArg(filters.SourceID))
	}
	if filters.TargetKind != "" {
		where = append(where, "target_kind = "+addArg(filters.TargetKind))
	}
	if filters.TargetID != "" {
		where = append(where, "target_id = "+addArg(filters.TargetID))
	}
	if filters.ResolutionState != "" {
		where = append(where, "resolution_state = "+addArg(filters.ResolutionState))
	}
	rows, err := s.pool.Query(ctx, `SELECT id, type, source_kind, source_id, target_kind, target_id, confidence, reason, evidence, resolution_state, created_at, updated_at
		FROM network_relationships WHERE `+strings.Join(where, " AND ")+` ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.NetworkRelationship
	for rows.Next() {
		rel, err := scanPostgresNetworkRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if out == nil {
		out = []domain.NetworkRelationship{}
	}
	return out, rows.Err()
}

func (s *Store) UpsertNetworkRelationship(ctx context.Context, in domain.CreateNetworkRelationship) (domain.NetworkRelationship, error) {
	if in.Type == "" || in.SourceKind == "" || in.SourceID == "" || in.TargetKind == "" || in.TargetID == "" {
		return domain.NetworkRelationship{}, fmt.Errorf("relationship type, source, and target are required: %w", storage.ErrValidation)
	}
	if in.ID == "" {
		in.ID = generatedNetworkRelationshipID(in)
	}
	if in.Confidence == 0 {
		in.Confidence = 1
	}
	requestedState := in.ResolutionState
	if in.ResolutionState == "" {
		in.ResolutionState = "open"
	}
	evidence, _ := json.Marshal(in.Evidence)
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `INSERT INTO network_relationships
		(id, organization_id, type, source_kind, source_id, target_kind, target_id, confidence, reason, evidence, resolution_state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13)
		ON CONFLICT(id) DO UPDATE SET
			type = EXCLUDED.type,
			source_kind = EXCLUDED.source_kind,
			source_id = EXCLUDED.source_id,
			target_kind = EXCLUDED.target_kind,
			target_id = EXCLUDED.target_id,
			confidence = EXCLUDED.confidence,
			reason = EXCLUDED.reason,
			evidence = EXCLUDED.evidence,
			resolution_state = CASE WHEN $14 = '' THEN network_relationships.resolution_state ELSE EXCLUDED.resolution_state END,
			updated_at = EXCLUDED.updated_at`,
		in.ID, s.orgID, string(in.Type), in.SourceKind, in.SourceID, in.TargetKind, in.TargetID, in.Confidence, in.Reason, string(evidence), in.ResolutionState, now, now, requestedState)
	if err != nil {
		return domain.NetworkRelationship{}, err
	}
	rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		return domain.NetworkRelationship{}, err
	}
	for _, rel := range rels {
		if rel.ID == in.ID {
			return rel, nil
		}
	}
	return domain.NetworkRelationship{}, storage.ErrNotFound
}

func (s *Store) UpdateNetworkRelationshipState(ctx context.Context, id string, state string, reason string) (domain.NetworkRelationship, bool, error) {
	if strings.TrimSpace(state) == "" {
		return domain.NetworkRelationship{}, false, fmt.Errorf("resolution_state is required: %w", storage.ErrValidation)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE network_relationships
		SET resolution_state = $1, reason = CASE WHEN $2 = '' THEN reason ELSE $2 END, updated_at = $3
		WHERE id = $4 AND organization_id = $5`, state, reason, time.Now().UTC(), id, s.orgID)
	if err != nil {
		return domain.NetworkRelationship{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.NetworkRelationship{}, false, nil
	}
	rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		return domain.NetworkRelationship{}, false, err
	}
	for _, rel := range rels {
		if rel.ID == id {
			return rel, true, nil
		}
	}
	return domain.NetworkRelationship{}, false, nil
}

func generatedNetworkRelationshipID(in domain.CreateNetworkRelationship) string {
	raw := strings.Join([]string{string(in.Type), in.SourceKind, in.SourceID, in.TargetKind, in.TargetID}, "\x00")
	sum := sha1.Sum([]byte(raw))
	return "rel-" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func scanPostgresNetworkObject(row pgx.Row) (domain.NetworkObject, error) {
	var obj domain.NetworkObject
	var metadata []byte
	var sourceID *uuid.UUID
	if err := row.Scan(&obj.ID, &obj.ObjectType, &obj.Provider, &obj.AccountID, &obj.Region, &obj.Name, &obj.CIDR, &obj.IPAddress, &obj.ProviderResourceID, &obj.ParentObjectID, &obj.PoolID, &sourceID, &obj.State, &metadata, &obj.CreatedAt, &obj.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return domain.NetworkObject{}, storage.ErrNotFound
		}
		return domain.NetworkObject{}, err
	}
	obj.SourceDiscoveredID = sourceID
	_ = json.Unmarshal(metadata, &obj.Metadata)
	return obj, nil
}

func scanPostgresNetworkRelationship(row pgx.Row) (domain.NetworkRelationship, error) {
	var rel domain.NetworkRelationship
	var evidence []byte
	if err := row.Scan(&rel.ID, &rel.Type, &rel.SourceKind, &rel.SourceID, &rel.TargetKind, &rel.TargetID, &rel.Confidence, &rel.Reason, &evidence, &rel.ResolutionState, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return domain.NetworkRelationship{}, storage.ErrNotFound
		}
		return domain.NetworkRelationship{}, err
	}
	_ = json.Unmarshal(evidence, &rel.Evidence)
	return rel, nil
}
