package domain

import (
	"time"
)

// RecommendationType categorizes the kind of recommendation.
type RecommendationType string

const (
	RecommendationTypeAllocation  RecommendationType = "allocation"
	RecommendationTypeCompliance  RecommendationType = "compliance"
	// Future types (stubbed):
	// RecommendationTypeConsolidation RecommendationType = "consolidation"
	// RecommendationTypeResize        RecommendationType = "resize"
	// RecommendationTypeReclaim       RecommendationType = "reclaim"
)

// RecommendationStatus tracks the lifecycle of a recommendation.
type RecommendationStatus string

const (
	RecommendationStatusPending   RecommendationStatus = "pending"
	RecommendationStatusApplied   RecommendationStatus = "applied"
	RecommendationStatusDismissed RecommendationStatus = "dismissed"
)

// RecommendationPriority indicates urgency.
type RecommendationPriority string

const (
	RecommendationPriorityHigh   RecommendationPriority = "high"
	RecommendationPriorityMedium RecommendationPriority = "medium"
	RecommendationPriorityLow    RecommendationPriority = "low"
)

// Recommendation represents an actionable suggestion derived from network analysis.
type Recommendation struct {
	ID            string                 `json:"id"`
	PoolID        int64                  `json:"pool_id"`
	Type          RecommendationType     `json:"type"`
	Status        RecommendationStatus   `json:"status"`
	Priority      RecommendationPriority `json:"priority"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	SuggestedCIDR string                 `json:"suggested_cidr,omitempty"`
	RuleID        string                 `json:"rule_id,omitempty"`
	Score         int                    `json:"score"`
	Metadata      map[string]string      `json:"metadata,omitempty"`
	DismissReason string                 `json:"dismiss_reason,omitempty"`
	AppliedPoolID *int64                 `json:"applied_pool_id,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// GenerateRecommendationsRequest is the input for generating recommendations.
type GenerateRecommendationsRequest struct {
	PoolIDs          []int64 `json:"pool_ids"`
	IncludeChildren  bool    `json:"include_children"`
	DesiredPrefixLen int     `json:"desired_prefix_len,omitempty"`
}

// GenerateRecommendationsResponse is the output of the generate endpoint.
type GenerateRecommendationsResponse struct {
	Items []Recommendation `json:"items"`
	Total int              `json:"total"`
}

// RecommendationFilters for listing recommendations.
type RecommendationFilters struct {
	PoolID   int64
	Type     string
	Status   string
	Priority string
	Page     int
	PageSize int
}

// RecommendationsListResponse is the paginated list response.
type RecommendationsListResponse struct {
	Items    []Recommendation `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// ApplyRecommendationRequest is the input for applying a recommendation.
type ApplyRecommendationRequest struct {
	Name      string `json:"name,omitempty"`
	AccountID *int64 `json:"account_id,omitempty"`
}

// DismissRecommendationRequest is the input for dismissing a recommendation.
type DismissRecommendationRequest struct {
	Reason string `json:"reason,omitempty"`
}
