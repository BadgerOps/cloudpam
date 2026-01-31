//go:build sqlite

package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // CGO-less SQLite driver
)

// SQLiteAuditLogger is a SQLite-backed implementation of AuditLogger.
type SQLiteAuditLogger struct {
	db *sql.DB
}

// NewSQLiteAuditLogger creates a new SQLite-backed audit logger.
// It shares the same database as the main store.
func NewSQLiteAuditLogger(dsn string) (*SQLiteAuditLogger, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteAuditLogger{db: db}, nil
}

// NewSQLiteAuditLoggerFromDB creates a new SQLite-backed audit logger using an existing DB connection.
func NewSQLiteAuditLoggerFromDB(db *sql.DB) *SQLiteAuditLogger {
	return &SQLiteAuditLogger{db: db}
}

// Close closes the database connection.
func (s *SQLiteAuditLogger) Close() error {
	return s.db.Close()
}

// Log records an audit event to the database.
func (s *SQLiteAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return nil
	}

	// Assign ID and timestamp if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Serialize changes to JSON
	var changesJSON sql.NullString
	if event.Changes != nil {
		data, err := json.Marshal(event.Changes)
		if err == nil {
			changesJSON = sql.NullString{String: string(data), Valid: true}
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, timestamp, actor, actor_type, action, resource_type, resource_id, resource_name, changes, request_id, ip_address, status_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		event.ID,
		event.Timestamp.Format(time.RFC3339Nano),
		event.Actor,
		event.ActorType,
		event.Action,
		event.ResourceType,
		event.ResourceID,
		sql.NullString{String: event.ResourceName, Valid: event.ResourceName != ""},
		changesJSON,
		sql.NullString{String: event.RequestID, Valid: event.RequestID != ""},
		sql.NullString{String: event.IPAddress, Valid: event.IPAddress != ""},
		event.StatusCode,
	)
	return err
}

// List retrieves audit events with optional filtering.
func (s *SQLiteAuditLogger) List(ctx context.Context, opts ListOptions) ([]*AuditEvent, int, error) {
	// Build WHERE clause
	where := "1=1"
	args := []any{}

	if opts.Actor != "" {
		where += " AND actor = ?"
		args = append(args, opts.Actor)
	}
	if opts.Action != "" {
		where += " AND action = ?"
		args = append(args, opts.Action)
	}
	if opts.ResourceType != "" {
		where += " AND resource_type = ?"
		args = append(args, opts.ResourceType)
	}
	if opts.Since != nil {
		where += " AND timestamp >= ?"
		args = append(args, opts.Since.Format(time.RFC3339Nano))
	}
	if opts.Until != nil {
		where += " AND timestamp <= ?"
		args = append(args, opts.Until.Format(time.RFC3339Nano))
	}

	// Count total
	var total int
	countQuery := "SELECT COUNT(*) FROM audit_logs WHERE " + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000
	}

	query := "SELECT id, timestamp, actor, actor_type, action, resource_type, resource_id, resource_name, changes, request_id, ip_address, status_code FROM audit_logs WHERE " + where + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		var timestamp string
		var resourceName, changesJSON, requestID, ipAddress sql.NullString

		if err := rows.Scan(&e.ID, &timestamp, &e.Actor, &e.ActorType, &e.Action, &e.ResourceType, &e.ResourceID, &resourceName, &changesJSON, &requestID, &ipAddress, &e.StatusCode); err != nil {
			return nil, 0, err
		}

		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		e.ResourceName = resourceName.String
		e.RequestID = requestID.String
		e.IPAddress = ipAddress.String

		if changesJSON.Valid && changesJSON.String != "" {
			var changes Changes
			if err := json.Unmarshal([]byte(changesJSON.String), &changes); err == nil {
				e.Changes = &changes
			}
		}

		events = append(events, &e)
	}

	return events, total, rows.Err()
}

// GetByResource retrieves audit events for a specific resource.
func (s *SQLiteAuditLogger) GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	events, _, err := s.List(ctx, ListOptions{
		ResourceType: resourceType,
		Limit:        1000,
	})
	if err != nil {
		return nil, err
	}

	// Filter by resource ID
	var filtered []*AuditEvent
	for _, e := range events {
		if e.ResourceID == resourceID {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}
