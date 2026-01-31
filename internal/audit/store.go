// Package audit provides audit logging functionality for CloudPAM.
// This file defines the store interface for audit log persistence.
package audit

import (
	"context"
	"time"
)

// AuditStore defines the interface for audit log persistence.
// This interface is designed to support both in-memory and database-backed
// implementations (PostgreSQL, SQLite).
type AuditStore interface {
	// Log records an audit event.
	// The implementation should assign an ID and timestamp if not provided.
	Log(ctx context.Context, event *AuditEvent) error

	// Query retrieves audit events matching the specified criteria.
	// Returns the matching events and the total count (for pagination).
	Query(ctx context.Context, opts QueryOptions) ([]*AuditEvent, int, error)

	// Count returns the number of events matching the specified criteria.
	// This is more efficient than Query when only the count is needed.
	Count(ctx context.Context, opts QueryOptions) (int64, error)

	// GetByID retrieves a single audit event by its ID.
	// Returns nil, nil if not found.
	GetByID(ctx context.Context, id string) (*AuditEvent, error)

	// GetByResource retrieves all audit events for a specific resource.
	GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error)

	// GetByActor retrieves audit events for a specific actor.
	GetByActor(ctx context.Context, actor string, limit int) ([]*AuditEvent, error)

	// Delete removes an audit event by ID.
	// This is primarily for retention policy enforcement.
	Delete(ctx context.Context, id string) error

	// DeleteOlderThan removes all audit events older than the specified time.
	// Returns the number of deleted events.
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)

	// Close releases any resources held by the store.
	Close() error
}

// QueryOptions provides flexible filtering and pagination for audit queries.
type QueryOptions struct {
	// Time range filters
	StartTime *time.Time // Events on or after this time
	EndTime   *time.Time // Events on or before this time

	// Actor filters
	ActorID   string   // Exact match on actor ID (e.g., API key prefix)
	ActorType string   // Filter by actor type: "api_key", "anonymous"
	ActorIDs  []string // Match any of these actor IDs

	// Action filters
	Action  string   // Exact match on action: "create", "update", "delete"
	Actions []string // Match any of these actions

	// Resource filters
	ResourceType string   // Filter by resource type: "pool", "account", "api_key"
	ResourceID   string   // Filter by exact resource ID
	ResourceIDs  []string // Match any of these resource IDs

	// Status filters
	Success    *bool // Filter by success (StatusCode < 400)
	StatusCode *int  // Exact match on status code
	MinStatus  *int  // Minimum status code (inclusive)
	MaxStatus  *int  // Maximum status code (inclusive)

	// Request filters
	RequestID string // Filter by request ID
	IPAddress string // Filter by IP address

	// Text search
	SearchText string // Full-text search across relevant fields

	// Pagination
	Limit  int // Maximum number of events to return (0 = default limit)
	Offset int // Number of events to skip (for pagination)

	// Ordering
	OrderBy   string // Sort field: "timestamp", "action", "actor_id", "resource_type"
	OrderDesc bool   // Sort in descending order (default: true for timestamp)
}

// Validate validates the query options and applies defaults.
func (o *QueryOptions) Validate() error {
	// Apply default limit if not set
	if o.Limit <= 0 {
		o.Limit = 50
	}

	// Cap limit to prevent excessive queries
	if o.Limit > 10000 {
		o.Limit = 10000
	}

	// Ensure offset is non-negative
	if o.Offset < 0 {
		o.Offset = 0
	}

	// Apply default ordering
	if o.OrderBy == "" {
		o.OrderBy = "timestamp"
		o.OrderDesc = true // Newest first by default
	}

	return nil
}

// DefaultQueryOptions returns sensible default query options.
func DefaultQueryOptions() QueryOptions {
	return QueryOptions{
		Limit:     50,
		Offset:    0,
		OrderBy:   "timestamp",
		OrderDesc: true,
	}
}

// WithTimeRange returns a copy of options with the specified time range.
func (o QueryOptions) WithTimeRange(start, end time.Time) QueryOptions {
	o.StartTime = &start
	o.EndTime = &end
	return o
}

// WithActor returns a copy of options filtered by actor.
func (o QueryOptions) WithActor(actorID string) QueryOptions {
	o.ActorID = actorID
	return o
}

// WithAction returns a copy of options filtered by action.
func (o QueryOptions) WithAction(action string) QueryOptions {
	o.Action = action
	return o
}

// WithResource returns a copy of options filtered by resource.
func (o QueryOptions) WithResource(resourceType, resourceID string) QueryOptions {
	o.ResourceType = resourceType
	o.ResourceID = resourceID
	return o
}

// WithPagination returns a copy of options with pagination settings.
func (o QueryOptions) WithPagination(limit, offset int) QueryOptions {
	o.Limit = limit
	o.Offset = offset
	return o
}

// WithOrdering returns a copy of options with ordering settings.
func (o QueryOptions) WithOrdering(orderBy string, desc bool) QueryOptions {
	o.OrderBy = orderBy
	o.OrderDesc = desc
	return o
}

// AuditRetentionPolicy defines retention settings for audit logs.
type AuditRetentionPolicy struct {
	// MaxAge is the maximum age of audit events to retain.
	// Events older than this will be deleted.
	MaxAge time.Duration

	// MaxEvents is the maximum number of events to retain.
	// When exceeded, oldest events are deleted.
	MaxEvents int64

	// RetainSuccessful controls whether successful operations are retained.
	// Setting to false deletes successful operations after a shorter period.
	RetainSuccessful bool

	// SuccessfulMaxAge is the retention period for successful operations
	// when RetainSuccessful is true.
	SuccessfulMaxAge time.Duration
}

// DefaultRetentionPolicy returns the default audit retention policy.
// By default, events are retained for 90 days.
func DefaultRetentionPolicy() AuditRetentionPolicy {
	return AuditRetentionPolicy{
		MaxAge:           90 * 24 * time.Hour, // 90 days
		MaxEvents:        10000000,            // 10 million events
		RetainSuccessful: true,
		SuccessfulMaxAge: 90 * 24 * time.Hour,
	}
}

// RetentionEnforcer handles automatic cleanup of old audit events.
type RetentionEnforcer interface {
	// EnforcePolicy applies the retention policy, deleting old events.
	// Returns the number of deleted events.
	EnforcePolicy(ctx context.Context, policy AuditRetentionPolicy) (int64, error)

	// GetStats returns statistics about the audit log.
	GetStats(ctx context.Context) (*AuditStats, error)
}

// AuditStats contains statistics about the audit log.
type AuditStats struct {
	// TotalEvents is the total number of audit events.
	TotalEvents int64

	// EventsByAction breaks down events by action type.
	EventsByAction map[string]int64

	// EventsByResource breaks down events by resource type.
	EventsByResource map[string]int64

	// EventsByActor breaks down events by actor (top actors).
	EventsByActor map[string]int64

	// OldestEvent is the timestamp of the oldest event.
	OldestEvent *time.Time

	// NewestEvent is the timestamp of the newest event.
	NewestEvent *time.Time

	// SuccessCount is the number of successful operations.
	SuccessCount int64

	// FailureCount is the number of failed operations.
	FailureCount int64
}

// ExtendedAuditStore combines AuditStore with retention management.
type ExtendedAuditStore interface {
	AuditStore
	RetentionEnforcer
}
