package domain

import "time"

// PoolType represents the hierarchical type of a pool.
type PoolType string

const (
	PoolTypeSupernet    PoolType = "supernet"
	PoolTypeRegion      PoolType = "region"
	PoolTypeEnvironment PoolType = "environment"
	PoolTypeVPC         PoolType = "vpc"
	PoolTypeSubnet      PoolType = "subnet"
)

// ValidPoolTypes contains all valid pool types for validation.
var ValidPoolTypes = []PoolType{
	PoolTypeSupernet,
	PoolTypeRegion,
	PoolTypeEnvironment,
	PoolTypeVPC,
	PoolTypeSubnet,
}

// IsValidPoolType checks if a pool type is valid.
func IsValidPoolType(t PoolType) bool {
	for _, valid := range ValidPoolTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// PoolStatus represents the lifecycle status of a pool.
type PoolStatus string

const (
	PoolStatusPlanned    PoolStatus = "planned"
	PoolStatusActive     PoolStatus = "active"
	PoolStatusDeprecated PoolStatus = "deprecated"
)

// ValidPoolStatuses contains all valid pool statuses for validation.
var ValidPoolStatuses = []PoolStatus{
	PoolStatusPlanned,
	PoolStatusActive,
	PoolStatusDeprecated,
}

// IsValidPoolStatus checks if a pool status is valid.
func IsValidPoolStatus(s PoolStatus) bool {
	for _, valid := range ValidPoolStatuses {
		if s == valid {
			return true
		}
	}
	return false
}

// PoolSource represents how a pool was created.
type PoolSource string

const (
	PoolSourceManual     PoolSource = "manual"
	PoolSourceDiscovered PoolSource = "discovered"
	PoolSourceImported   PoolSource = "imported"
)

// ValidPoolSources contains all valid pool sources for validation.
var ValidPoolSources = []PoolSource{
	PoolSourceManual,
	PoolSourceDiscovered,
	PoolSourceImported,
}

// IsValidPoolSource checks if a pool source is valid.
func IsValidPoolSource(s PoolSource) bool {
	for _, valid := range ValidPoolSources {
		if s == valid {
			return true
		}
	}
	return false
}

// Pool represents an address pool that can contain nested sub-pools.
type Pool struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	CIDR        string            `json:"cidr"`
	ParentID    *int64            `json:"parent_id,omitempty"`
	AccountID   *int64            `json:"account_id,omitempty"`
	Type        PoolType          `json:"type"`
	Status      PoolStatus        `json:"status"`
	Source      PoolSource        `json:"source"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	DeletedAt   *time.Time        `json:"deleted_at,omitempty"`
}

// PoolStats contains computed statistics for a pool.
type PoolStats struct {
	TotalIPs       int64   `json:"total_ips"`
	UsedIPs        int64   `json:"used_ips"`
	AvailableIPs   int64   `json:"available_ips"`
	Utilization    float64 `json:"utilization"` // 0-100 percentage
	ChildCount     int     `json:"child_count"`
	DirectChildren int     `json:"direct_children"`
	// Projection is filled only by GET /pools/{id}/stats.
	Projection *GrowthProjection `json:"projection,omitempty"`
}

// ProjectionStatus says whether a GrowthProjection carries a usable rate.
type ProjectionStatus string

const (
	// ProjectionOK means the fit found positive growth and a finite runway.
	ProjectionOK ProjectionStatus = "ok"
	// ProjectionNoGrowth means usage was flat or shrinking over the window.
	ProjectionNoGrowth ProjectionStatus = "no_growth"
	// ProjectionInsufficientHistory means the window holds too little history to fit.
	ProjectionInsufficientHistory ProjectionStatus = "insufficient_history"
)

// GrowthProjection extrapolates a pool's recent allocation rate to a runway
// and a usage forecast.
type GrowthProjection struct {
	Status      ProjectionStatus `json:"status"`
	WindowDays  int              `json:"window_days"`
	WindowStart time.Time        `json:"window_start"`
	SampleCount int              `json:"sample_count"`
	// GrowthPerDay is the fitted slope in IPs per day; negative means shrinking.
	GrowthPerDay float64 `json:"growth_ips_per_day"`
	// RunwayDays and ExhaustionDate are set only when Status is ok.
	RunwayDays     *float64         `json:"runway_days,omitempty"`
	ExhaustionDate *time.Time       `json:"exhaustion_date,omitempty"`
	Forecast       []ProjectedUsage `json:"forecast"`
}

// ProjectedUsage is the expected usage of a pool a number of months ahead.
type ProjectedUsage struct {
	Months      int     `json:"months"`
	UsedIPs     int64   `json:"used_ips"`
	Utilization float64 `json:"utilization"` // 0-100 percentage
}

// PoolWithStats combines a Pool with its computed statistics.
type PoolWithStats struct {
	Pool
	Stats    PoolStats       `json:"stats"`
	Children []PoolWithStats `json:"children,omitempty"`
}

// CreatePool is the input for creating a pool.
type CreatePool struct {
	Name        string            `json:"name"`
	CIDR        string            `json:"cidr"`
	ParentID    *int64            `json:"parent_id,omitempty"`
	AccountID   *int64            `json:"account_id,omitempty"`
	Type        PoolType          `json:"type,omitempty"`
	Status      PoolStatus        `json:"status,omitempty"`
	Source      PoolSource        `json:"source,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// UpdatePool is the input for updating a pool. It is an already-resolved
// command, not a wire type: the PATCH handler inspects key presence in the raw
// request body and passes the resolved value in here.
type UpdatePool struct {
	Name *string `json:"name,omitempty"`
	// AccountID intentionally has no omitempty. Unlike the other fields, nil
	// does not mean "leave unchanged" — every store writes it unconditionally,
	// so nil clears the assignment. Omitting it from the marshaled form would
	// make "clear the account" indistinguishable from "not specified".
	AccountID   *int64             `json:"account_id"`
	Type        *PoolType          `json:"type,omitempty"`
	Status      *PoolStatus        `json:"status,omitempty"`
	Description *string            `json:"description,omitempty"`
	Tags        *map[string]string `json:"tags,omitempty"`
}

// Account represents a cloud account or project to which pools can be assigned.
// It uses a generic shape to support AWS accounts, GCP projects, etc.
type Account struct {
	ID          int64      `json:"id"`
	Key         string     `json:"key"` // unique key like "aws:123456789012" or "gcp:my-project"
	Name        string     `json:"name"`
	Provider    string     `json:"provider,omitempty"`
	ExternalID  string     `json:"external_id,omitempty"`
	Description string     `json:"description,omitempty"`
	Platform    string     `json:"platform,omitempty"`
	Tier        string     `json:"tier,omitempty"`
	Environment string     `json:"environment,omitempty"`
	Regions     []string   `json:"regions,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

// CreateAccount is the input for creating an account.
type CreateAccount struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider,omitempty"`
	ExternalID  string   `json:"external_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Tier        string   `json:"tier,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Regions     []string `json:"regions,omitempty"`
}

// UtilizationSnapshot captures pool utilization at a point in time
// for historical tracking and growth projections.
type UtilizationSnapshot struct {
	ID           int64     `json:"id"`
	PoolID       int64     `json:"pool_id"`
	TotalIPs     int64     `json:"total_ips"`
	UsedIPs      int64     `json:"used_ips"`
	AvailableIPs int64     `json:"available_ips"`
	Utilization  float64   `json:"utilization"` // 0-100 percentage
	ChildCount   int       `json:"child_count"`
	CapturedAt   time.Time `json:"captured_at"`
}
