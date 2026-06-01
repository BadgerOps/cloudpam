package audit

import (
	"context"
	"fmt"
)

// Sink receives a copy of an audit event after it has been persisted.
type Sink interface {
	Send(ctx context.Context, event *AuditEvent) error
}

// ForwardingErrorHandler observes best-effort forwarding failures.
type ForwardingErrorHandler func(ctx context.Context, event *AuditEvent, err error)

// ForwardingAuditLogger persists events through a primary logger and forwards
// successfully persisted events to secondary sinks.
type ForwardingAuditLogger struct {
	primary AuditLogger
	sinks   []Sink
	onError ForwardingErrorHandler
}

// NewForwardingAuditLogger wraps a primary audit logger with best-effort sinks.
func NewForwardingAuditLogger(primary AuditLogger, sinks []Sink, onError ForwardingErrorHandler) *ForwardingAuditLogger {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &ForwardingAuditLogger{
		primary: primary,
		sinks:   filtered,
		onError: onError,
	}
}

// Log records an audit event and best-effort forwards it to configured sinks.
func (f *ForwardingAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return nil
	}
	if f.primary == nil {
		return fmt.Errorf("primary audit logger is nil")
	}
	if err := f.primary.Log(ctx, event); err != nil {
		return err
	}

	eventCopy := copyEvent(event)
	for _, sink := range f.sinks {
		if err := sink.Send(ctx, eventCopy); err != nil && f.onError != nil {
			f.onError(ctx, eventCopy, err)
		}
	}
	return nil
}

// List delegates audit queries to the primary logger.
func (f *ForwardingAuditLogger) List(ctx context.Context, opts ListOptions) ([]*AuditEvent, int, error) {
	if f.primary == nil {
		return nil, 0, fmt.Errorf("primary audit logger is nil")
	}
	return f.primary.List(ctx, opts)
}

// GetByResource delegates audit queries to the primary logger.
func (f *ForwardingAuditLogger) GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	if f.primary == nil {
		return nil, fmt.Errorf("primary audit logger is nil")
	}
	return f.primary.GetByResource(ctx, resourceType, resourceID)
}
