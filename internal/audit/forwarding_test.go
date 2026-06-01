package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type captureSink struct {
	events []*AuditEvent
	err    error
}

func (s *captureSink) Send(_ context.Context, event *AuditEvent) error {
	s.events = append(s.events, copyEvent(event))
	return s.err
}

func TestForwardingAuditLoggerForwardsPersistedEvent(t *testing.T) {
	primary := NewMemoryAuditLogger()
	sink := &captureSink{}
	logger := NewForwardingAuditLogger(primary, []Sink{sink}, nil)

	event := &AuditEvent{
		Actor:        "admin",
		ActorType:    ActorTypeUser,
		Action:       ActionCreate,
		ResourceType: ResourcePool,
		ResourceID:   "pool-1",
		StatusCode:   201,
	}
	if err := logger.Log(context.Background(), event); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	if event.ID == "" || event.Timestamp.IsZero() {
		t.Fatal("primary logger should assign ID and timestamp before forwarding")
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 forwarded event, got %d", len(sink.events))
	}
	if sink.events[0].ID != event.ID {
		t.Fatalf("forwarded ID = %q, want %q", sink.events[0].ID, event.ID)
	}
	if sink.events[0].Action != ActionCreate {
		t.Fatalf("forwarded action = %q, want %q", sink.events[0].Action, ActionCreate)
	}
}

func TestForwardingAuditLoggerSinkErrorsAreBestEffort(t *testing.T) {
	primary := NewMemoryAuditLogger()
	sinkErr := errors.New("send failed")
	var observed error
	logger := NewForwardingAuditLogger(primary, []Sink{&captureSink{err: sinkErr}}, func(_ context.Context, _ *AuditEvent, err error) {
		observed = err
	})

	err := logger.Log(context.Background(), &AuditEvent{
		Action:       ActionDelete,
		ResourceType: ResourceAccount,
		ResourceID:   "account-1",
		StatusCode:   204,
	})
	if err != nil {
		t.Fatalf("sink errors should not fail Log(), got %v", err)
	}
	if !errors.Is(observed, sinkErr) {
		t.Fatalf("observed error = %v, want %v", observed, sinkErr)
	}
	_, total, err := primary.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("persisted events = %d, want 1", total)
	}
}

func TestCEFFormatterEscapesAndMapsFields(t *testing.T) {
	event := &AuditEvent{
		ID:           "evt-1",
		Timestamp:    time.Unix(1700000000, 123000000).UTC(),
		Actor:        `admin=user\one`,
		ActorType:    ActorTypeUser,
		Action:       ActionUpdate,
		ResourceType: "pool|segment",
		ResourceID:   "pool-1",
		ResourceName: "prod\ncore",
		RequestID:    "req-1",
		IPAddress:    "192.0.2.10",
		StatusCode:   200,
	}

	got := (CEFFormatter{DeviceVersion: "0.15.0"}).Format(event)
	wantParts := []string{
		`CEF:0|BadgerOps|CloudPAM|0.15.0|update|pool\|segment.update|3|`,
		`act=update`,
		`outcome=success`,
		`rt=1700000000123`,
		`externalId=evt-1`,
		`suser=admin\=user\\one`,
		`cs1Label=actor_type cs1=user`,
		`src=192.0.2.10`,
		`request=req-1`,
		`cs2Label=resource_type cs2=pool|segment`,
		`cs3Label=resource_id cs3=pool-1`,
		`cs4Label=resource_name cs4=prod\ncore`,
		`cn1Label=http_status cn1=200`,
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("CEF output missing %q:\n%s", part, got)
		}
	}
}
