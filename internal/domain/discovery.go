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
	Query        string
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

// DiscoveryImportPreviewRequest asks the API to classify selected discovered
// resources before any managed IPAM records are created or linked.
type DiscoveryImportPreviewRequest struct {
	AccountID   int64       `json:"account_id"`
	ResourceIDs []uuid.UUID `json:"resource_ids"`
	PoolID      *int64      `json:"pool_id,omitempty"`
}

// DiscoveryImportApplyRequest applies the same selected-resource import plan
// returned by preview. Only importable rows are created or linked.
type DiscoveryImportApplyRequest struct {
	AccountID   int64       `json:"account_id"`
	ResourceIDs []uuid.UUID `json:"resource_ids"`
	PoolID      *int64      `json:"pool_id,omitempty"`
}

// DiscoveryImportPreviewItem describes the proposed conversion, soft link, or
// blocking issue for one selected discovered resource.
type DiscoveryImportPreviewItem struct {
	ResourceID           uuid.UUID         `json:"resource_id"`
	ProviderResourceID   string            `json:"provider_resource_id,omitempty"`
	Name                 string            `json:"name,omitempty"`
	Provider             string            `json:"provider,omitempty"`
	Region               string            `json:"region,omitempty"`
	ResourceType         CloudResourceType `json:"resource_type,omitempty"`
	CIDR                 string            `json:"cidr,omitempty"`
	Status               string            `json:"status"`
	ProposedAction       string            `json:"proposed_action"`
	ProposedManagedType  string            `json:"proposed_managed_type,omitempty"`
	LinkedPoolID         *int64            `json:"linked_pool_id,omitempty"`
	ProposedPoolID       *int64            `json:"proposed_pool_id,omitempty"`
	ProposedParentPoolID *int64            `json:"proposed_parent_pool_id,omitempty"`
	Issues               []string          `json:"issues"`
	Evidence             []string          `json:"evidence,omitempty"`
	ConflictPoolIDs      []int64           `json:"conflict_pool_ids,omitempty"`
	DuplicateResourceIDs []uuid.UUID       `json:"duplicate_resource_ids,omitempty"`
}

// DiscoveryImportPreviewResponse is returned by import preview and by apply as
// the authoritative per-row classification used for the operation.
type DiscoveryImportPreviewResponse struct {
	Items          []DiscoveryImportPreviewItem `json:"items"`
	Importable     int                          `json:"importable"`
	Blocked        int                          `json:"blocked"`
	LinkedOnly     int                          `json:"linked_only"`
	AlreadyLinked  int                          `json:"already_linked"`
	ConflictCount  int                          `json:"conflict_count"`
	SelectedPoolID *int64                       `json:"selected_pool_id,omitempty"`
}

// DiscoveryImportApplyResponse reports the result of applying selected,
// importable discovery preview rows.
type DiscoveryImportApplyResponse struct {
	Preview           DiscoveryImportPreviewResponse `json:"preview"`
	PoolsCreated      int                            `json:"pools_created"`
	ResourcesLinked   int                            `json:"resources_linked"`
	Skipped           int                            `json:"skipped"`
	Errors            []string                       `json:"errors"`
	CreatedPoolIDs    []int64                        `json:"created_pool_ids,omitempty"`
	LinkedResourceIDs []uuid.UUID                    `json:"linked_resource_ids,omitempty"`
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
	ID             uuid.UUID           `json:"id"`
	Name           string              `json:"name"`
	AccountID      int64               `json:"account_id"`
	APIKeyID       string              `json:"api_key_id"`
	Status         AgentStatus         `json:"status"` // computed at read time (healthy/stale/offline)
	ApprovalStatus AgentApprovalStatus `json:"approval_status"`
	Version        string              `json:"version"`
	Hostname       string              `json:"hostname"`
	LastSeenAt     time.Time           `json:"last_seen_at"`
	RegisteredAt   *time.Time          `json:"registered_at,omitempty"`
	ApprovedAt     *time.Time          `json:"approved_at,omitempty"`
	ApprovedBy     *string             `json:"approved_by,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
}

// IngestRequest is the request body for the /api/v1/discovery/ingest endpoint.
type IngestRequest struct {
	AccountID int64                `json:"account_id"`
	Resources []DiscoveredResource `json:"resources"`
	AgentID   *uuid.UUID           `json:"agent_id,omitempty"`
	SyncJobID *uuid.UUID           `json:"sync_job_id,omitempty"`
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

// AgentHeartbeatResponse is returned to agents after each heartbeat.
// A sync job means the server has requested an immediate agent-side scan.
type AgentHeartbeatResponse struct {
	Status    string     `json:"status"`
	SyncJobID *uuid.UUID `json:"sync_job_id,omitempty"`
	AccountID int64      `json:"account_id,omitempty"`
}

// DiscoveryAgentsResponse is the response for listing discovery agents.
type DiscoveryAgentsResponse struct {
	Items []DiscoveryAgent `json:"items"`
}

// AgentProvisionRequest is the request body for provisioning a new agent.
// An admin creates a provisioning bundle that includes an API key for the agent.
type AgentProvisionRequest struct {
	Name string `json:"name"` // human-readable agent name
}

// AgentProvisionBundle is the base64-encoded JSON blob given to the agent.
// The agent decodes this to configure itself.
type AgentProvisionBundle struct {
	AgentName string `json:"agent_name"`
	APIKey    string `json:"api_key"`
	ServerURL string `json:"server_url"`
}

// AgentProvisionResponse is the API response when provisioning an agent.
type AgentProvisionResponse struct {
	AgentName string `json:"agent_name"`
	APIKey    string `json:"api_key"`    // plaintext, shown once
	APIKeyID  string `json:"api_key_id"` // for management
	ServerURL string `json:"server_url"`
	Token     string `json:"token"` // base64-encoded AgentProvisionBundle
}

// AgentRegisterRequest is the request body for agent self-registration.
// The agent calls this on first startup, authenticated via its provisioned API key.
// account_id is derived from the cloud environment (e.g. AWS STS GetCallerIdentity).
// agent_id is auto-generated by the agent.
type AgentRegisterRequest struct {
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	AccountID int64     `json:"account_id"` // derived from cloud env
	Version   string    `json:"version,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
}

// AgentRegisterResponse is the response body for agent self-registration.
type AgentRegisterResponse struct {
	AgentID        uuid.UUID           `json:"agent_id"`
	ApprovalStatus AgentApprovalStatus `json:"approval_status"`
	Message        string              `json:"message,omitempty"`
}

// OrgAccountIngest represents a single AWS account's discovered resources for bulk org ingest.
type OrgAccountIngest struct {
	AWSAccountID string               `json:"aws_account_id"`
	AccountName  string               `json:"account_name"`
	AccountEmail string               `json:"account_email"`
	Provider     string               `json:"provider"`
	Regions      []string             `json:"regions"`
	Resources    []DiscoveredResource `json:"resources"`
}

// BulkIngestRequest is the request body for the /api/v1/discovery/ingest/org endpoint.
type BulkIngestRequest struct {
	Accounts  []OrgAccountIngest `json:"accounts"`
	AgentID   string             `json:"agent_id,omitempty"`
	SyncJobID *uuid.UUID         `json:"sync_job_id,omitempty"`
}

// BulkIngestResponse is the response body for the /api/v1/discovery/ingest/org endpoint.
type BulkIngestResponse struct {
	AccountsProcessed int      `json:"accounts_processed"`
	AccountsCreated   int      `json:"accounts_created"`
	TotalResources    int      `json:"total_resources"`
	Errors            []string `json:"errors,omitempty"`
}
