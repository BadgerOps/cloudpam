package domain

import (
	"time"

	"github.com/google/uuid"
)

// CloudResourceType represents the type of a discovered cloud resource.
type CloudResourceType string

const (
	ResourceTypeVPC              CloudResourceType = "vpc"
	ResourceTypeSubnet           CloudResourceType = "subnet"
	ResourceTypeNetworkInterface CloudResourceType = "network_interface"
	ResourceTypeElasticIP        CloudResourceType = "elastic_ip"
)

// ValidCloudResourceTypes contains all valid cloud resource types.
var ValidCloudResourceTypes = []CloudResourceType{
	ResourceTypeVPC,
	ResourceTypeSubnet,
	ResourceTypeNetworkInterface,
	ResourceTypeElasticIP,
}

// IsValidCloudResourceType checks if a cloud resource type is valid.
func IsValidCloudResourceType(t CloudResourceType) bool {
	for _, valid := range ValidCloudResourceTypes {
		if t == valid {
			return true
		}
	}
	return false
}

// DiscoveryStatus represents the current state of a discovered resource.
type DiscoveryStatus string

const (
	DiscoveryStatusActive  DiscoveryStatus = "active"
	DiscoveryStatusStale   DiscoveryStatus = "stale"
	DiscoveryStatusDeleted DiscoveryStatus = "deleted"
)

// ValidDiscoveryStatuses contains all valid discovery statuses.
var ValidDiscoveryStatuses = []DiscoveryStatus{
	DiscoveryStatusActive,
	DiscoveryStatusStale,
	DiscoveryStatusDeleted,
}

// IsValidDiscoveryStatus checks if a discovery status is valid.
func IsValidDiscoveryStatus(s DiscoveryStatus) bool {
	for _, valid := range ValidDiscoveryStatuses {
		if s == valid {
			return true
		}
	}
	return false
}

// SyncJobStatus represents the status of a discovery sync job.
type SyncJobStatus string

const (
	SyncJobStatusPending   SyncJobStatus = "pending"
	SyncJobStatusRunning   SyncJobStatus = "running"
	SyncJobStatusCompleted SyncJobStatus = "completed"
	SyncJobStatusFailed    SyncJobStatus = "failed"
)

// DiscoveredResource represents a cloud resource found during discovery.
type DiscoveredResource struct {
	ID               uuid.UUID         `json:"id"`
	AccountID        int64             `json:"account_id"`
	Provider         string            `json:"provider"`
	Region           string            `json:"region"`
	ResourceType     CloudResourceType `json:"resource_type"`
	ResourceID       string            `json:"resource_id"`
	Name             string            `json:"name"`
	CIDR             string            `json:"cidr,omitempty"`
	ParentResourceID *string           `json:"parent_resource_id,omitempty"`
	PoolID           *int64            `json:"pool_id,omitempty"`
	Status           DiscoveryStatus   `json:"status"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	DiscoveredAt     time.Time         `json:"discovered_at"`
	LastSeenAt       time.Time         `json:"last_seen_at"`
}

// SyncJob tracks a discovery sync run for a cloud account.
type SyncJob struct {
	ID               uuid.UUID     `json:"id"`
	AccountID        int64         `json:"account_id"`
	Status           SyncJobStatus `json:"status"`
	StartedAt        *time.Time    `json:"started_at,omitempty"`
	CompletedAt      *time.Time    `json:"completed_at,omitempty"`
	ResourcesFound   int           `json:"resources_found"`
	ResourcesCreated int           `json:"resources_created"`
	ResourcesUpdated int           `json:"resources_updated"`
	ResourcesDeleted int           `json:"resources_deleted"`
	ErrorMessage     string        `json:"error_message,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
}

// DiscoveryFilters for querying discovered resources.
type DiscoveryFilters struct {
	Provider     string
	Region       string
	ResourceType string
	Status       string
	HasPool      *bool // nil = any, true = linked, false = unlinked
	Page         int
	PageSize     int
}

// DiscoveryResourcesResponse is the paginated response for discovered resources.
type DiscoveryResourcesResponse struct {
	Items    []DiscoveredResource `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// SyncJobsResponse is the response for listing sync jobs.
type SyncJobsResponse struct {
	Items []SyncJob `json:"items"`
}
