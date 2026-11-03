package domain

import (
	"time"

	"github.com/google/uuid"
)

// DriftType categorizes what kind of mismatch was found.
type DriftType string

const (
	DriftTypeUnmanaged    DriftType = "unmanaged"
	DriftTypeCIDRMismatch DriftType = "cidr_mismatch"
	DriftTypeOrphanedPool DriftType = "orphaned_pool"
	DriftTypeNameMismatch DriftType = "name_mismatch"
	DriftTypeAccountDrift DriftType = "account_drift"
)

// DriftSeverity indicates urgency.
type DriftSeverity string

const (
	DriftSeverityCritical DriftSeverity = "critical"
	DriftSeverityWarning  DriftSeverity = "warning"
	DriftSeverityInfo     DriftSeverity = "info"
)

// DriftStatus tracks lifecycle.
type DriftStatus string

const (
	DriftStatusOpen     DriftStatus = "open"
	DriftStatusResolved DriftStatus = "resolved"
	DriftStatusIgnored  DriftStatus = "ignored"
)

// DriftAction is the reconciliation step suggested for an open drift item.
type DriftAction string

const (
	// DriftActionLinkPool links the discovered resource to SuggestedPoolID,
	// a managed pool with exactly the resource's CIDR.
	DriftActionLinkPool DriftAction = "link_pool"
	// DriftActionCreatePool imports the discovered resource as a new pool under
	// SuggestedPoolID, its most specific containing pool, or at the top level
	// when SuggestedPoolID is nil.
	DriftActionCreatePool DriftAction = "create_pool"
	// DriftActionDeprecatePool marks pool PoolID deprecated because the
	// discovered resource it was created from is no longer active.
	DriftActionDeprecatePool DriftAction = "deprecate_pool"
	// DriftActionReview means no automatic action is safe; SuggestionReason
	// says why.
	DriftActionReview DriftAction = "review"
)

// DriftItem represents a single detected drift between cloud state and IPAM state.
type DriftItem struct {
	ID           string            `json:"id"`
	AccountID    int64             `json:"account_id"`
	ResourceID   *uuid.UUID        `json:"resource_id,omitempty"`
	PoolID       *int64            `json:"pool_id,omitempty"`
	Type         DriftType         `json:"type"`
	Severity     DriftSeverity     `json:"severity"`
	Status       DriftStatus       `json:"status"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	ResourceCIDR string            `json:"resource_cidr,omitempty"`
	PoolCIDR     string            `json:"pool_cidr,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
	IgnoreReason string            `json:"ignore_reason,omitempty"`
	// The suggestion fields are derived from current pools and resources each
	// time an open item is read, so they are never persisted.
	SuggestedAction  DriftAction `json:"suggested_action,omitempty"`
	SuggestedPoolID  *int64      `json:"suggested_pool_id,omitempty"`
	SuggestionReason string      `json:"suggestion_reason,omitempty"`
	ResolvedAt       *time.Time  `json:"resolved_at,omitempty"`
	DetectedAt       time.Time   `json:"detected_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// DriftSummary provides aggregate counts for a drift detection run.
type DriftSummary struct {
	TotalDrifts      int            `json:"total_drifts"`
	BySeverity       map[string]int `json:"by_severity"`
	ByType           map[string]int `json:"by_type"`
	AccountsScanned  int            `json:"accounts_scanned"`
	ResourcesScanned int            `json:"resources_scanned"`
	PoolsScanned     int            `json:"pools_scanned"`
}

// RunDriftDetectionRequest is the input for running drift detection.
type RunDriftDetectionRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

// RunDriftDetectionResponse is the output of a drift detection run.
type RunDriftDetectionResponse struct {
	Items   []DriftItem  `json:"items"`
	Total   int          `json:"total"`
	Summary DriftSummary `json:"summary"`
}

// DriftFilters for listing drift items.
type DriftFilters struct {
	AccountID int64
	Type      string
	Severity  string
	Status    string
	Page      int
	PageSize  int
}

// DriftListResponse is the paginated list of drift items.
type DriftListResponse struct {
	Items    []DriftItem  `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Summary  DriftSummary `json:"summary"`
}

// IgnoreDriftRequest is the input for ignoring a drift item.
type IgnoreDriftRequest struct {
	Reason string `json:"reason,omitempty"`
}
