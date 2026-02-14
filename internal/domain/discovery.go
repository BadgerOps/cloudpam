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
	Source           string        `json:"source"` // "local" or "agent"
	AgentID          *uuid.UUID    `json:"agent_id,omitempty"`
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

// AgentStatus represents the health status of a discovery agent.
type AgentStatus string

const (
	AgentStatusHealthy AgentStatus = "healthy" // last_seen < 5 minutes ago
	AgentStatusStale   AgentStatus = "stale"   // last_seen 5-15 minutes ago
	AgentStatusOffline AgentStatus = "offline" // last_seen > 15 minutes ago
)

// AgentApprovalStatus represents the approval state of a discovery agent.
type AgentApprovalStatus string

const (
	AgentApprovalPending  AgentApprovalStatus = "pending_approval"
	AgentApprovalApproved AgentApprovalStatus = "approved"
	AgentApprovalRejected AgentApprovalStatus = "rejected"
)

// DiscoveryAgent represents a remote discovery agent.
type DiscoveryAgent struct {
	ID                uuid.UUID           `json:"id"`
	Name              string              `json:"name"`
	AccountID         int64               `json:"account_id"`
	APIKeyID          string              `json:"api_key_id"`
	Status            AgentStatus         `json:"status"` // computed at read time (healthy/stale/offline)
	ApprovalStatus    AgentApprovalStatus `json:"approval_status"`
	BootstrapTokenID  *string             `json:"bootstrap_token_id,omitempty"`
	Version           string              `json:"version"`
	Hostname          string              `json:"hostname"`
	LastSeenAt        time.Time           `json:"last_seen_at"`
	RegisteredAt      *time.Time          `json:"registered_at,omitempty"`
	ApprovedAt        *time.Time          `json:"approved_at,omitempty"`
	ApprovedBy        *string             `json:"approved_by,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
}

// IngestRequest is the request body for the /api/v1/discovery/ingest endpoint.
type IngestRequest struct {
	AccountID int64                `json:"account_id"`
	Resources []DiscoveredResource `json:"resources"`
	AgentID   *uuid.UUID           `json:"agent_id,omitempty"`
}

// IngestResponse is the response body for the /api/v1/discovery/ingest endpoint.
type IngestResponse struct {
	JobID            uuid.UUID `json:"job_id"`
	ResourcesFound   int       `json:"resources_found"`
	ResourcesCreated int       `json:"resources_created"`
	ResourcesUpdated int       `json:"resources_updated"`
	ResourcesDeleted int       `json:"resources_deleted"`
}

// AgentHeartbeatRequest is the request body for agent heartbeat.
type AgentHeartbeatRequest struct {
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	AccountID int64     `json:"account_id"`
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname"`
}

// DiscoveryAgentsResponse is the response for listing discovery agents.
type DiscoveryAgentsResponse struct {
	Items []DiscoveryAgent `json:"items"`
}

// BootstrapToken represents a token for agent self-registration.
type BootstrapToken struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token,omitempty"` // only returned on creation
	TokenHash []byte     `json:"-"`
	AccountID *int64     `json:"account_id,omitempty"` // nil = any account
	CreatedBy string     `json:"created_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Revoked   bool       `json:"revoked"`
	UsedCount int        `json:"used_count"`
	MaxUses   *int       `json:"max_uses,omitempty"` // nil = unlimited
	CreatedAt time.Time  `json:"created_at"`
}

// IsValid returns true if the token can still be used.
func (t *BootstrapToken) IsValid() bool {
	if t.Revoked {
		return false
	}
	if t.ExpiresAt != nil && time.Now().UTC().After(*t.ExpiresAt) {
		return false
	}
	if t.MaxUses != nil && t.UsedCount >= *t.MaxUses {
		return false
	}
	return true
}

// AgentRegisterRequest is the request body for agent self-registration.
type AgentRegisterRequest struct {
	Name           string `json:"name"`
	AccountID      int64  `json:"account_id"`
	BootstrapToken string `json:"bootstrap_token"`
	Version        string `json:"version,omitempty"`
	Hostname       string `json:"hostname,omitempty"`
}

// AgentRegisterResponse is the response body for agent self-registration.
type AgentRegisterResponse struct {
	AgentID        uuid.UUID           `json:"agent_id"`
	APIKey         string              `json:"api_key,omitempty"` // returned if auto-approved
	ApprovalStatus AgentApprovalStatus `json:"approval_status"`
	Message        string              `json:"message,omitempty"`
}

// BootstrapTokenCreateRequest is the request body for creating a bootstrap token.
type BootstrapTokenCreateRequest struct {
	Name       string  `json:"name"`
	AccountID  *int64  `json:"account_id,omitempty"`
	ExpiresIn  *string `json:"expires_in,omitempty"` // duration string like "24h", "7d"
	MaxUses    *int    `json:"max_uses,omitempty"`
}

// BootstrapTokensResponse is the response for listing bootstrap tokens.
type BootstrapTokensResponse struct {
	Items []BootstrapToken `json:"items"`
}

// AgentConfigTemplateResponse is the response for the config template endpoint.
type AgentConfigTemplateResponse struct {
	Config string `json:"config"` // YAML content
}
