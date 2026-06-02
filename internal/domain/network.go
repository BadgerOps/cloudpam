package domain

import "github.com/google/uuid"

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
	ID                 string         `json:"id"`
	ParentID           *string        `json:"parent_id,omitempty"`
	Kind               string         `json:"kind"`
	ObjectType         string         `json:"object_type"`
	Name               string         `json:"name"`
	CIDR               string         `json:"cidr,omitempty"`
	IPAddress          string         `json:"ip_address,omitempty"`
	Provider           string         `json:"provider,omitempty"`
	AccountID          *int64         `json:"account_id,omitempty"`
	AccountName        string         `json:"account_name,omitempty"`
	Region             string         `json:"region,omitempty"`
	ProviderResourceID string         `json:"provider_resource_id,omitempty"`
	DiscoveredID       *uuid.UUID     `json:"discovered_id,omitempty"`
	LinkedPoolID       *int64         `json:"linked_pool_id,omitempty"`
	Source             string         `json:"source"`
	State              string         `json:"state"`
	Issues             []NetworkIssue `json:"issues,omitempty"`
	Evidence           []string       `json:"evidence,omitempty"`
	Children           []NetworkNode  `json:"children,omitempty"`
}

// NetworkConflict is a computed conflict/drift/duplicate record shown in the
// network conflict panel.
type NetworkConflict struct {
	ID                  string      `json:"id"`
	Type                string      `json:"type"`
	Severity            string      `json:"severity"`
	Status              string      `json:"status"`
	Title               string      `json:"title"`
	Description         string      `json:"description"`
	RecommendedAction   string      `json:"recommended_action,omitempty"`
	NodeIDs             []string    `json:"node_ids,omitempty"`
	DiscoveredIDs       []uuid.UUID `json:"discovered_ids,omitempty"`
	PoolIDs             []int64     `json:"pool_ids,omitempty"`
	Provider            string      `json:"provider,omitempty"`
	AccountIDs          []int64     `json:"account_ids,omitempty"`
	Regions             []string    `json:"regions,omitempty"`
	ObjectTypes         []string    `json:"object_types,omitempty"`
	CIDR                string      `json:"cidr,omitempty"`
	Evidence            []string    `json:"evidence,omitempty"`
	AvailableDecisions  []string    `json:"available_decisions,omitempty"`
	ResolutionState     string      `json:"resolution_state,omitempty"`
	ResolutionRequested string      `json:"resolution_requested,omitempty"`
}

// NetworkViewResponse returns the flat or hierarchical merged network view.
type NetworkViewResponse struct {
	Items         []NetworkNode     `json:"items"`
	Total         int               `json:"total"`
	ConflictCount int               `json:"conflict_count"`
	Conflicts     []NetworkConflict `json:"conflicts,omitempty"`
}

// NetworkConflictListResponse returns computed network conflicts.
type NetworkConflictListResponse struct {
	Items []NetworkConflict `json:"items"`
	Total int               `json:"total"`
}

// ResolveNetworkConflictRequest asks the API to resolve or mark a computed
// conflict decision. The current implementation returns the requested state as
// response metadata; durable conflict records are handled by drift items.
type ResolveNetworkConflictRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}
