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
}

// PoolStats contains computed statistics for a pool.
type PoolStats struct {
	TotalIPs       int64   `json:"total_ips"`
	UsedIPs        int64   `json:"used_ips"`
	AvailableIPs   int64   `json:"available_ips"`
	Utilization    float64 `json:"utilization"` // 0-100 percentage
	ChildCount     int     `json:"child_count"`
	DirectChildren int     `json:"direct_children"`
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

// UpdatePool is the input for updating a pool.
type UpdatePool struct {
	Name        *string            `json:"name,omitempty"`
	AccountID   *int64             `json:"account_id"`
	Type        *PoolType          `json:"type,omitempty"`
	Status      *PoolStatus        `json:"status,omitempty"`
	Description *string            `json:"description,omitempty"`
	Tags        *map[string]string `json:"tags,omitempty"`
}

// Account represents a cloud account or project to which pools can be assigned.
// It uses a generic shape to support AWS accounts, GCP projects, etc.
type Account struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"` // unique key like "aws:123456789012" or "gcp:my-project"
	Name        string    `json:"name"`
	Provider    string    `json:"provider,omitempty"`
	ExternalID  string    `json:"external_id,omitempty"`
	Description string    `json:"description,omitempty"`
	Platform    string    `json:"platform,omitempty"`
	Tier        string    `json:"tier,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Regions     []string  `json:"regions,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
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
