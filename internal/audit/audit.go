// Package audit provides audit logging functionality for CloudPAM.
// It captures and stores audit events for security and compliance purposes.
package audit

import (
	"context"
	"time"
)

// AuditEvent represents a single auditable action in the system.
type AuditEvent struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Actor        string    `json:"actor"`         // API key prefix or "anonymous"
	ActorType    string    `json:"actor_type"`    // "api_key" or "anonymous"
	Action       string    `json:"action"`        // "create", "update", "delete"
	ResourceType string    `json:"resource_type"` // "pool", "account", "api_key"
	ResourceID   string    `json:"resource_id"`
	ResourceName string    `json:"resource_name,omitempty"`
	Changes      *Changes  `json:"changes,omitempty"` // before/after for updates
	RequestID    string    `json:"request_id,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	StatusCode   int       `json:"status_code"`
}

// Changes captures the before and after state for update operations.
type Changes struct {
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
}

// ListOptions provides filtering and pagination options for listing audit events.
type ListOptions struct {
	Limit        int
	Offset       int
	Actor        string
	Action       string
	ResourceType string
	Since        *time.Time
	Until        *time.Time
}

// AuditLogger defines the interface for audit logging operations.
type AuditLogger interface {
	// Log records an audit event.
	Log(ctx context.Context, event *AuditEvent) error

	// List retrieves audit events with optional filtering.
	List(ctx context.Context, opts ListOptions) ([]*AuditEvent, int, error)

	// GetByResource retrieves audit events for a specific resource.
	GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error)
}

// Valid actions for audit events.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionRead   = "read" // Used only for sensitive operations like key listing
)

// Valid resource types for audit events.
const (
	ResourcePool    = "pool"
	ResourceAccount = "account"
	ResourceAPIKey  = "api_key"
)

// Valid actor types.
const (
	ActorTypeAPIKey    = "api_key"
	ActorTypeAnonymous = "anonymous"
)
