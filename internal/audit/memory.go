package audit

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultMaxEvents is the default maximum number of events to store.
const DefaultMaxEvents = 10000

// MemoryAuditLogger is an in-memory implementation of AuditLogger.
// It stores events in a slice with newest events first.
// This implementation is thread-safe and limits storage to prevent unbounded growth.
type MemoryAuditLogger struct {
	mu        sync.RWMutex
	events    []*AuditEvent
	maxEvents int
}

// MemoryAuditLoggerOption configures a MemoryAuditLogger.
type MemoryAuditLoggerOption func(*MemoryAuditLogger)

// WithMaxEvents sets the maximum number of events to store.
func WithMaxEvents(max int) MemoryAuditLoggerOption {
	return func(m *MemoryAuditLogger) {
		if max > 0 {
			m.maxEvents = max
		}
	}
}

// NewMemoryAuditLogger creates a new in-memory audit logger.
func NewMemoryAuditLogger(opts ...MemoryAuditLoggerOption) *MemoryAuditLogger {
	m := &MemoryAuditLogger{
		events:    make([]*AuditEvent, 0),
		maxEvents: DefaultMaxEvents,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Log records an audit event.
func (m *MemoryAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Assign ID and timestamp if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Create a copy to prevent external modification
	eventCopy := *event
	if event.Changes != nil {
		eventCopy.Changes = &Changes{
			Before: copyMap(event.Changes.Before),
			After:  copyMap(event.Changes.After),
		}
	}

	// Prepend to slice (newest first)
	m.events = append([]*AuditEvent{&eventCopy}, m.events...)

	// Trim to maxEvents
	if len(m.events) > m.maxEvents {
		m.events = m.events[:m.maxEvents]
	}

	return nil
}

// List retrieves audit events with optional filtering.
// Returns the filtered events, total count, and any error.
func (m *MemoryAuditLogger) List(ctx context.Context, opts ListOptions) ([]*AuditEvent, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Apply filters
	var filtered []*AuditEvent
	for _, e := range m.events {
		if !matchesFilters(e, opts) {
			continue
		}
		filtered = append(filtered, e)
	}

	total := len(filtered)

	// Apply pagination
	if opts.Limit <= 0 {
		opts.Limit = 50 // default limit
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000 // max limit
	}

	start := opts.Offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + opts.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	result := filtered[start:end]

	// Return copies to prevent external modification
	copies := make([]*AuditEvent, len(result))
	for i, e := range result {
		copies[i] = copyEvent(e)
	}

	return copies, total, nil
}

// GetByResource retrieves audit events for a specific resource.
func (m *MemoryAuditLogger) GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*AuditEvent
	for _, e := range m.events {
		if e.ResourceType == resourceType && e.ResourceID == resourceID {
			result = append(result, copyEvent(e))
		}
	}

	return result, nil
}

// matchesFilters checks if an event matches the provided filter options.
func matchesFilters(e *AuditEvent, opts ListOptions) bool {
	if opts.Actor != "" && e.Actor != opts.Actor {
		return false
	}
	if opts.Action != "" && e.Action != opts.Action {
		return false
	}
	if opts.ResourceType != "" && e.ResourceType != opts.ResourceType {
		return false
	}
	if opts.Since != nil && e.Timestamp.Before(*opts.Since) {
		return false
	}
	if opts.Until != nil && e.Timestamp.After(*opts.Until) {
		return false
	}
	return true
}

// copyEvent creates a deep copy of an audit event.
func copyEvent(e *AuditEvent) *AuditEvent {
	if e == nil {
		return nil
	}
	copy := *e
	if e.Changes != nil {
		copy.Changes = &Changes{
			Before: copyMap(e.Changes.Before),
			After:  copyMap(e.Changes.After),
		}
	}
	return &copy
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	copy := make(map[string]any, len(m))
	for k, v := range m {
		copy[k] = v
	}
	return copy
}
