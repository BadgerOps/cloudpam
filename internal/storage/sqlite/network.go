//go:build sqlite

package sqlite

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.NetworkStore = (*Store)(nil)

func (s *Store) ListNetworkObjects(ctx context.Context, filters domain.NetworkObjectFilters) ([]domain.NetworkObject, error) {
	where := []string{"1=1"}
	args := []any{}
	if filters.AccountID > 0 {
		where = append(where, "account_id = ?")
		args = append(args, filters.AccountID)
	}
	if filters.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filters.Provider)
	}
	if filters.Region != "" {
		where = append(where, "region = ?")
		args = append(args, filters.Region)
	}
	if filters.ObjectType != "" {
		where = append(where, "object_type = ?")
		args = append(args, filters.ObjectType)
	}
	if filters.State != "" {
		where = append(where, "state = ?")
		args = append(args, filters.State)
	}
	if filters.PoolID > 0 {
		where = append(where, "pool_id = ?")
		args = append(args, filters.PoolID)
	}
	if filters.SourceDiscoveredID != "" {
		where = append(where, "source_discovered_id = ?")
		args = append(args, filters.SourceDiscoveredID)
	}
	if filters.Query != "" {
		q := "%" + strings.ToLower(filters.Query) + "%"
		where = append(where, "(LOWER(name) LIKE ? OR LOWER(cidr) LIKE ? OR LOWER(ip_address) LIKE ? OR LOWER(provider_resource_id) LIKE ?)")
		args = append(args, q, q, q, q)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at
		FROM network_objects WHERE `+strings.Join(where, " AND ")+` ORDER BY account_id ASC, region ASC, name ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.NetworkObject
	for rows.Next() {
		obj, err := scanNetworkObject(rows)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at
		FROM network_objects WHERE id = ?`, id)
	obj, err := scanNetworkObject(row)
	if err != nil {
		if err == sql.ErrNoRows {
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
	metadata := "{}"
	if in.Metadata != nil {
		if b, err := json.Marshal(in.Metadata); err == nil {
			metadata = string(b)
		}
	}
	var discoveredID *string
	if in.SourceDiscoveredID != nil {
		id := in.SourceDiscoveredID.String()
		discoveredID = &id
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `INSERT INTO network_objects
		(object_type, provider, account_id, region, name, cidr, ip_address, provider_resource_id, parent_object_id, pool_id, source_discovered_id, state, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(objectType), in.Provider, in.AccountID, in.Region, in.Name, in.CIDR, in.IPAddress, in.ProviderResourceID,
		in.ParentObjectID, in.PoolID, discoveredID, string(state), metadata, now, now)
	if err != nil {
		return domain.NetworkObject{}, err
	}
	id, err := res.LastInsertId()
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
	metadata := "{}"
	if obj.Metadata != nil {
		if b, err := json.Marshal(obj.Metadata); err == nil {
			metadata = string(b)
		}
	}
	var discoveredID *string
	if obj.SourceDiscoveredID != nil {
		id := obj.SourceDiscoveredID.String()
		discoveredID = &id
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `UPDATE network_objects SET
		object_type = ?, provider = ?, account_id = ?, region = ?, name = ?, cidr = ?, ip_address = ?, provider_resource_id = ?,
		parent_object_id = ?, pool_id = ?, source_discovered_id = ?, state = ?, metadata = ?, updated_at = ?
		WHERE id = ?`,
		string(obj.ObjectType), obj.Provider, obj.AccountID, obj.Region, obj.Name, obj.CIDR, obj.IPAddress, obj.ProviderResourceID,
		obj.ParentObjectID, obj.PoolID, discoveredID, string(obj.State), metadata, now, id)
	if err != nil {
		return domain.NetworkObject{}, false, err
	}
	obj, _, err = s.GetNetworkObject(ctx, id)
	return obj, true, err
}

func (s *Store) ListNetworkRelationships(ctx context.Context, filters domain.NetworkRelationshipFilters) ([]domain.NetworkRelationship, error) {
	where := []string{"1=1"}
	args := []any{}
	if filters.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filters.Type)
	}
	if filters.SourceKind != "" {
		where = append(where, "source_kind = ?")
		args = append(args, filters.SourceKind)
	}
	if filters.SourceID != "" {
		where = append(where, "source_id = ?")
		args = append(args, filters.SourceID)
	}
	if filters.TargetKind != "" {
		where = append(where, "target_kind = ?")
		args = append(args, filters.TargetKind)
	}
	if filters.TargetID != "" {
		where = append(where, "target_id = ?")
		args = append(args, filters.TargetID)
	}
	if filters.ResolutionState != "" {
		where = append(where, "resolution_state = ?")
		args = append(args, filters.ResolutionState)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, source_kind, source_id, target_kind, target_id, confidence, reason, evidence, resolution_state, created_at, updated_at
		FROM network_relationships WHERE `+strings.Join(where, " AND ")+` ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.NetworkRelationship
	for rows.Next() {
		rel, err := scanNetworkRelationship(rows)
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
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO network_relationships
		(id, type, source_kind, source_id, target_kind, target_id, confidence, reason, evidence, resolution_state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			source_kind = excluded.source_kind,
			source_id = excluded.source_id,
			target_kind = excluded.target_kind,
			target_id = excluded.target_id,
			confidence = excluded.confidence,
			reason = excluded.reason,
			evidence = excluded.evidence,
			resolution_state = CASE WHEN ? = '' THEN network_relationships.resolution_state ELSE excluded.resolution_state END,
			updated_at = excluded.updated_at`,
		in.ID, string(in.Type), in.SourceKind, in.SourceID, in.TargetKind, in.TargetID, in.Confidence, in.Reason, string(evidence), in.ResolutionState, now, now, requestedState)
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
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE network_relationships SET resolution_state = ?, reason = CASE WHEN ? = '' THEN reason ELSE ? END, updated_at = ? WHERE id = ?`, state, reason, reason, now, id)
	if err != nil {
		return domain.NetworkRelationship{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
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

type scanner interface {
	Scan(dest ...any) error
}

func scanNetworkObject(row scanner) (domain.NetworkObject, error) {
	var obj domain.NetworkObject
	var parentID, poolID sql.NullInt64
	var sourceID, metadata, createdAt, updatedAt string
	if err := row.Scan(&obj.ID, &obj.ObjectType, &obj.Provider, &obj.AccountID, &obj.Region, &obj.Name, &obj.CIDR, &obj.IPAddress, &obj.ProviderResourceID, &parentID, &poolID, &sourceID, &obj.State, &metadata, &createdAt, &updatedAt); err != nil {
		return domain.NetworkObject{}, err
	}
	if parentID.Valid {
		obj.ParentObjectID = &parentID.Int64
	}
	if poolID.Valid {
		obj.PoolID = &poolID.Int64
	}
	if sourceID != "" {
		id, err := uuid.Parse(sourceID)
		if err == nil {
			obj.SourceDiscoveredID = &id
		}
	}
	_ = json.Unmarshal([]byte(metadata), &obj.Metadata)
	obj.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	obj.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return obj, nil
}

func scanNetworkRelationship(row scanner) (domain.NetworkRelationship, error) {
	var rel domain.NetworkRelationship
	var evidence, createdAt, updatedAt string
	if err := row.Scan(&rel.ID, &rel.Type, &rel.SourceKind, &rel.SourceID, &rel.TargetKind, &rel.TargetID, &rel.Confidence, &rel.Reason, &evidence, &rel.ResolutionState, &createdAt, &updatedAt); err != nil {
		return domain.NetworkRelationship{}, err
	}
	_ = json.Unmarshal([]byte(evidence), &rel.Evidence)
	rel.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	rel.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return rel, nil
}
