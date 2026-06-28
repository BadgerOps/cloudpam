package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// NetworkObjectType is the provider-neutral type of a managed network object.
type NetworkObjectType string

const (
	NetworkObjectTypeVPC      NetworkObjectType = "vpc"
	NetworkObjectTypeSubnet   NetworkObjectType = "subnet"
	NetworkObjectTypeEIP      NetworkObjectType = "eip"
	NetworkObjectTypePublicIP NetworkObjectType = "public_ip"
	NetworkObjectTypeNetwork  NetworkObjectType = "network"
	NetworkObjectTypeOther    NetworkObjectType = "other"
)

// NetworkObjectState is the managed lifecycle state for a network object.
type NetworkObjectState string

const (
	NetworkObjectStateManaged     NetworkObjectState = "managed"
	NetworkObjectStatePlaceholder NetworkObjectState = "placeholder"
	NetworkObjectStateImported    NetworkObjectState = "imported"
	NetworkObjectStateIgnored     NetworkObjectState = "ignored"
)

// NetworkObject is a durable provider-neutral object such as a VPC, subnet,
// EIP/public IP, network, or future provider-specific object.
type NetworkObject struct {
	ID                 int64              `json:"id"`
	ObjectType         NetworkObjectType  `json:"object_type"`
	Provider           string             `json:"provider,omitempty"`
	AccountID          int64              `json:"account_id"`
	Region             string             `json:"region,omitempty"`
	Name               string             `json:"name"`
	CIDR               string             `json:"cidr,omitempty"`
	IPAddress          string             `json:"ip_address,omitempty"`
	ProviderResourceID string             `json:"provider_resource_id,omitempty"`
	ParentObjectID     *int64             `json:"parent_object_id,omitempty"`
	PoolID             *int64             `json:"pool_id,omitempty"`
	SourceDiscoveredID *uuid.UUID         `json:"source_discovered_id,omitempty"`
	State              NetworkObjectState `json:"state"`
	Metadata           map[string]string  `json:"metadata,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type CreateNetworkObject struct {
	ObjectType         NetworkObjectType  `json:"object_type"`
	Provider           string             `json:"provider,omitempty"`
	AccountID          int64              `json:"account_id"`
	Region             string             `json:"region,omitempty"`
	Name               string             `json:"name"`
	CIDR               string             `json:"cidr,omitempty"`
	IPAddress          string             `json:"ip_address,omitempty"`
	ProviderResourceID string             `json:"provider_resource_id,omitempty"`
	ParentObjectID     *int64             `json:"parent_object_id,omitempty"`
	PoolID             *int64             `json:"pool_id,omitempty"`
	SourceDiscoveredID *uuid.UUID         `json:"source_discovered_id,omitempty"`
	State              NetworkObjectState `json:"state,omitempty"`
	Metadata           map[string]string  `json:"metadata,omitempty"`
}

type UpdateNetworkObject struct {
	ObjectType         *NetworkObjectType  `json:"object_type,omitempty"`
	Provider           *string             `json:"provider,omitempty"`
	AccountID          *int64              `json:"account_id,omitempty"`
	Region             *string             `json:"region,omitempty"`
	Name               *string             `json:"name,omitempty"`
	CIDR               *string             `json:"cidr,omitempty"`
	IPAddress          *string             `json:"ip_address,omitempty"`
	ProviderResourceID *string             `json:"provider_resource_id,omitempty"`
	ParentObjectID     *int64              `json:"parent_object_id,omitempty"`
	PoolID             *int64              `json:"pool_id,omitempty"`
	SourceDiscoveredID *uuid.UUID          `json:"source_discovered_id,omitempty"`
	State              *NetworkObjectState `json:"state,omitempty"`
	Metadata           *map[string]string  `json:"metadata,omitempty"`
}

type NetworkObjectFilters struct {
	AccountID          int64
	Provider           string
	Region             string
	ObjectType         string
	State              string
	PoolID             int64
	SourceDiscoveredID string
	Query              string
}

type NetworkObjectListResponse struct {
	Items []NetworkObject `json:"items"`
	Total int             `json:"total"`
}

// NetworkRelationshipType is a durable link between merged-network entities.
type NetworkRelationshipType string

const (
	NetworkRelationshipContains        NetworkRelationshipType = "contains"
	NetworkRelationshipMatches         NetworkRelationshipType = "matches"
	NetworkRelationshipConflicts       NetworkRelationshipType = "conflicts"
	NetworkRelationshipMissingParent   NetworkRelationshipType = "missing_parent"
	NetworkRelationshipCandidateImport NetworkRelationshipType = "candidate_import"
	NetworkRelationshipImportedAs      NetworkRelationshipType = "imported_as"
	NetworkRelationshipDuplicateOf     NetworkRelationshipType = "duplicate_of"
)

type NetworkRelationship struct {
	ID              string                  `json:"id"`
	Type            NetworkRelationshipType `json:"type"`
	SourceKind      string                  `json:"source_kind"`
	SourceID        string                  `json:"source_id"`
	TargetKind      string                  `json:"target_kind"`
	TargetID        string                  `json:"target_id"`
	Confidence      float64                 `json:"confidence"`
	Reason          string                  `json:"reason,omitempty"`
	Evidence        []string                `json:"evidence,omitempty"`
	ResolutionState string                  `json:"resolution_state"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type CreateNetworkRelationship struct {
	ID              string                  `json:"id,omitempty"`
	Type            NetworkRelationshipType `json:"type"`
	SourceKind      string                  `json:"source_kind"`
	SourceID        string                  `json:"source_id"`
	TargetKind      string                  `json:"target_kind"`
	TargetID        string                  `json:"target_id"`
	Confidence      float64                 `json:"confidence,omitempty"`
	Reason          string                  `json:"reason,omitempty"`
	Evidence        []string                `json:"evidence,omitempty"`
	ResolutionState string                  `json:"resolution_state,omitempty"`
}

type NetworkRelationshipFilters struct {
	IDs             []string
	Type            string
	SourceKind      string
	SourceID        string
	TargetKind      string
	TargetID        string
	EntityKind      string
	EntityID        string
	ResolutionState string
}

type NetworkRelationshipListResponse struct {
	Items []NetworkRelationship `json:"items"`
	Total int                   `json:"total"`
}

type ResolveNetworkRelationshipRequest struct {
	ID              string `json:"id,omitempty"`
	ResolutionState string `json:"resolution_state"`
	Reason          string `json:"reason,omitempty"`
}

type NetworkSchemaPolicy struct {
	Name                string `json:"name"`
	OwnershipStrategy   string `json:"ownership_strategy"`
	DuplicateScope      string `json:"duplicate_scope"`
	HierarchyScope      string `json:"hierarchy_scope"`
	ManualRelationships bool   `json:"manual_relationships,omitempty"`
}

// DefaultNetworkSchemaPolicy returns the conservative merged-network policy.
func DefaultNetworkSchemaPolicy() NetworkSchemaPolicy {
	return NetworkSchemaPolicy{Name: "account_level", OwnershipStrategy: "account", DuplicateScope: "account", HierarchyScope: "account"}
}

// NormalizeNetworkSchemaPolicy fills derived policy fields and falls back to
// the conservative account-level policy when the name is empty or unknown.
func NormalizeNetworkSchemaPolicy(policy *NetworkSchemaPolicy) *NetworkSchemaPolicy {
	if policy == nil {
		defaults := DefaultNetworkSchemaPolicy()
		return &defaults
	}
	name := strings.ToLower(strings.TrimSpace(policy.Name))
	switch name {
	case "region_level":
		normalized := NetworkSchemaPolicy{Name: name, OwnershipStrategy: "region", DuplicateScope: "region", HierarchyScope: "region"}
		return &normalized
	case "global":
		normalized := NetworkSchemaPolicy{Name: name, OwnershipStrategy: "global", DuplicateScope: "global", HierarchyScope: "global"}
		return &normalized
	case "custom", "manual":
		normalized := NetworkSchemaPolicy{Name: name, OwnershipStrategy: "manual", DuplicateScope: "manual", HierarchyScope: "manual", ManualRelationships: true}
		if strings.ToLower(strings.TrimSpace(policy.DuplicateScope)) == "global" {
			normalized.DuplicateScope = "global"
		}
		return &normalized
	default:
		defaults := DefaultNetworkSchemaPolicy()
		return &defaults
	}
}

// ValidateNetworkSchemaPolicy returns a user-facing validation error, or an
// empty string when the policy can be persisted.
func ValidateNetworkSchemaPolicy(policy *NetworkSchemaPolicy) string {
	if policy == nil {
		return "policy is required"
	}
	name := strings.ToLower(strings.TrimSpace(policy.Name))
	switch name {
	case "account_level", "region_level", "global":
		normalized := NormalizeNetworkSchemaPolicy(policy)
		if policy.OwnershipStrategy != "" && strings.ToLower(strings.TrimSpace(policy.OwnershipStrategy)) != normalized.OwnershipStrategy {
			return "ownership_strategy does not match schema policy"
		}
		if policy.DuplicateScope != "" && strings.ToLower(strings.TrimSpace(policy.DuplicateScope)) != normalized.DuplicateScope {
			return "duplicate_scope does not match schema policy"
		}
		if policy.HierarchyScope != "" && strings.ToLower(strings.TrimSpace(policy.HierarchyScope)) != normalized.HierarchyScope {
			return "hierarchy_scope does not match schema policy"
		}
	case "custom", "manual":
		if policy.OwnershipStrategy != "" && strings.ToLower(strings.TrimSpace(policy.OwnershipStrategy)) != "manual" {
			return "ownership_strategy does not match schema policy"
		}
		duplicateScope := strings.ToLower(strings.TrimSpace(policy.DuplicateScope))
		if duplicateScope != "" && duplicateScope != "manual" && duplicateScope != "global" {
			return "duplicate_scope for manual policy must be manual or global"
		}
		if policy.HierarchyScope != "" && strings.ToLower(strings.TrimSpace(policy.HierarchyScope)) != "manual" {
			return "hierarchy_scope does not match schema policy"
		}
	default:
		return "schema policy must be account_level, region_level, global, manual, or custom"
	}
	return ""
}

// NetworkIssue describes an actionable issue attached to a merged network row
// or hierarchy node.
type NetworkIssue struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// NetworkNode is the provider-neutral representation used by merged flat and
// hierarchy network views.
type NetworkNode struct {
	ID                 string                `json:"id"`
	ParentID           *string               `json:"parent_id,omitempty"`
	Kind               string                `json:"kind"`
	ObjectType         string                `json:"object_type"`
	Name               string                `json:"name"`
	CIDR               string                `json:"cidr,omitempty"`
	IPAddress          string                `json:"ip_address,omitempty"`
	Provider           string                `json:"provider,omitempty"`
	AccountID          *int64                `json:"account_id,omitempty"`
	AccountName        string                `json:"account_name,omitempty"`
	Region             string                `json:"region,omitempty"`
	ProviderResourceID string                `json:"provider_resource_id,omitempty"`
	DiscoveredID       *uuid.UUID            `json:"discovered_id,omitempty"`
	LinkedPoolID       *int64                `json:"linked_pool_id,omitempty"`
	Source             string                `json:"source"`
	State              string                `json:"state"`
	Issues             []NetworkIssue        `json:"issues,omitempty"`
	Evidence           []string              `json:"evidence,omitempty"`
	Relationships      []NetworkRelationship `json:"relationships,omitempty"`
	Children           []NetworkNode         `json:"children,omitempty"`
}

// NetworkConflict is a computed conflict/drift/duplicate record shown in the
// network conflict panel.
type NetworkConflict struct {
	ID                  string                `json:"id"`
	Type                string                `json:"type"`
	Severity            string                `json:"severity"`
	Status              string                `json:"status"`
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	RecommendedAction   string                `json:"recommended_action,omitempty"`
	NodeIDs             []string              `json:"node_ids,omitempty"`
	DiscoveredIDs       []uuid.UUID           `json:"discovered_ids,omitempty"`
	PoolIDs             []int64               `json:"pool_ids,omitempty"`
	Provider            string                `json:"provider,omitempty"`
	AccountIDs          []int64               `json:"account_ids,omitempty"`
	Regions             []string              `json:"regions,omitempty"`
	ObjectTypes         []string              `json:"object_types,omitempty"`
	CIDR                string                `json:"cidr,omitempty"`
	Evidence            []string              `json:"evidence,omitempty"`
	Relationships       []NetworkRelationship `json:"relationships,omitempty"`
	AvailableDecisions  []string              `json:"available_decisions,omitempty"`
	ResolutionState     string                `json:"resolution_state,omitempty"`
	ResolutionRequested string                `json:"resolution_requested,omitempty"`
}

// NetworkViewResponse returns the flat or hierarchical merged network view.
type NetworkViewResponse struct {
	Items         []NetworkNode       `json:"items"`
	Total         int                 `json:"total"`
	ConflictCount int                 `json:"conflict_count"`
	Conflicts     []NetworkConflict   `json:"conflicts,omitempty"`
	SchemaPolicy  NetworkSchemaPolicy `json:"schema_policy"`
}

// NetworkConflictListResponse returns computed network conflicts.
type NetworkConflictListResponse struct {
	Items        []NetworkConflict   `json:"items"`
	Total        int                 `json:"total"`
	SchemaPolicy NetworkSchemaPolicy `json:"schema_policy"`
}

// ResolveNetworkConflictRequest asks the API to resolve or mark a computed
// conflict decision. The current implementation returns the requested state as
// response metadata; durable conflict records are handled by drift items.
type ResolveNetworkConflictRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// NetworkConflictLinkActionRequest links an affected discovered resource to a
// managed pool as a real conflict remediation.
type NetworkConflictLinkActionRequest struct {
	DiscoveredID uuid.UUID `json:"discovered_id"`
	PoolID       int64     `json:"pool_id"`
	Reason       string    `json:"reason,omitempty"`
	Override     bool      `json:"override,omitempty"`
}

// NetworkConflictImportActionRequest imports affected discovered resources as
// managed pools or links them through the discovery import apply workflow.
type NetworkConflictImportActionRequest struct {
	ResourceIDs []uuid.UUID `json:"resource_ids"`
	PoolID      *int64      `json:"pool_id,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Override    bool        `json:"override,omitempty"`
}

// NetworkConflictPlaceholderParentActionRequest creates a durable placeholder
// parent object for a missing-parent conflict.
type NetworkConflictPlaceholderParentActionRequest struct {
	DiscoveredID uuid.UUID `json:"discovered_id"`
	Name         string    `json:"name,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

// NetworkConflictActionResponse reports the mutation performed for a conflict
// action and the conflict state after persistence.
type NetworkConflictActionResponse struct {
	Conflict       NetworkConflict               `json:"conflict"`
	Action         string                        `json:"action"`
	ResourceLinked bool                          `json:"resource_linked,omitempty"`
	DiscoveredID   *uuid.UUID                    `json:"discovered_id,omitempty"`
	PoolID         *int64                        `json:"pool_id,omitempty"`
	PreviousPoolID *int64                        `json:"previous_pool_id,omitempty"`
	NetworkObject  *NetworkObject                `json:"network_object,omitempty"`
	Relationships  []NetworkRelationship         `json:"relationships,omitempty"`
	Import         *DiscoveryImportApplyResponse `json:"import,omitempty"`
}
