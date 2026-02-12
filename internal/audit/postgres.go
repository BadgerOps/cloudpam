//go:build postgres

package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultOrgID is the UUID of the default organization for single-tenant deployments.
const defaultOrgID = "00000000-0000-0000-0000-000000000001"

// PostgresAuditLogger is a PostgreSQL-backed implementation of AuditLogger.
type PostgresAuditLogger struct {
	pool    *pgxpool.Pool
	ownPool bool // true if we created the pool (and should close it)
	orgID   string
}

// NewPostgresAuditLogger creates a new PostgreSQL-backed audit logger with its own connection pool.
func NewPostgresAuditLogger(connStr string) (*PostgresAuditLogger, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresAuditLogger{pool: pool, ownPool: true, orgID: defaultOrgID}, nil
}

// NewPostgresAuditLoggerFromPool creates a new PostgreSQL-backed audit logger using an existing pool.
func NewPostgresAuditLoggerFromPool(pool *pgxpool.Pool) *PostgresAuditLogger {
	return &PostgresAuditLogger{pool: pool, ownPool: false, orgID: defaultOrgID}
}

// Close closes the database connection if we own it.
func (s *PostgresAuditLogger) Close() error {
	if s.ownPool {
		s.pool.Close()
	}
	return nil
}

// Log records an audit event to the database.
func (s *PostgresAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return nil
	}

	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	var changesJSON []byte
	if event.Changes != nil {
		var err error
		changesJSON, err = json.Marshal(event.Changes)
		if err != nil {
			changesJSON = nil
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (id, organization_id, timestamp, actor_id, actor_type, action,
			resource_type, resource_id, resource_name, changes, request_id, actor_ip, status_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13)`,
		event.ID, s.orgID, event.Timestamp,
		nullStr(event.Actor), event.ActorType, event.Action,
		event.ResourceType, event.ResourceID,
		nullStr(event.ResourceName),
		nullBytes(changesJSON),
		nullStr(event.RequestID),
		nullStr(event.IPAddress),
		event.StatusCode,
	)
	return err
}

// List retrieves audit events with optional filtering.
func (s *PostgresAuditLogger) List(ctx context.Context, opts ListOptions) ([]*AuditEvent, int, error) {
	where := "organization_id = $1"
	args := []any{s.orgID}
	argIdx := 2

	if opts.Actor != "" {
		where += " AND actor_id = $" + itoa(argIdx)
		args = append(args, opts.Actor)
		argIdx++
	}
	if opts.Action != "" {
		where += " AND action = $" + itoa(argIdx)
		args = append(args, opts.Action)
		argIdx++
	}
	if opts.ResourceType != "" {
		where += " AND resource_type = $" + itoa(argIdx)
		args = append(args, opts.ResourceType)
		argIdx++
	}
	if opts.Since != nil {
		where += " AND timestamp >= $" + itoa(argIdx)
		args = append(args, *opts.Since)
		argIdx++
	}
	if opts.Until != nil {
		where += " AND timestamp <= $" + itoa(argIdx)
		args = append(args, *opts.Until)
		argIdx++
	}

	// Count total
	var total int
	countQuery := "SELECT COUNT(*) FROM audit_events WHERE " + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000
	}

	query := "SELECT id, timestamp, actor_id, actor_type, action, resource_type, resource_id, resource_name, changes, request_id, actor_ip, status_code FROM audit_events WHERE " + where +
		" ORDER BY timestamp DESC LIMIT $" + itoa(argIdx) + " OFFSET $" + itoa(argIdx+1)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events, err := scanAuditEvents(rows)
	if err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetByResource retrieves audit events for a specific resource.
func (s *PostgresAuditLogger) GetByResource(ctx context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, timestamp, actor_id, actor_type, action, resource_type, resource_id,
			resource_name, changes, request_id, actor_ip, status_code
		FROM audit_events
		WHERE organization_id = $1 AND resource_type = $2 AND resource_id = $3
		ORDER BY timestamp DESC
		LIMIT 1000`, s.orgID, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAuditEvents(rows)
}

func scanAuditEvents(rows pgx.Rows) ([]*AuditEvent, error) {
	var events []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		var actor, resourceName, changesStr, requestID, ipAddress *string
		var statusCode *int

		if err := rows.Scan(
			&e.ID, &e.Timestamp, &actor, &e.ActorType,
			&e.Action, &e.ResourceType, &e.ResourceID,
			&resourceName, &changesStr, &requestID, &ipAddress, &statusCode,
		); err != nil {
			return nil, err
		}

		if actor != nil {
			e.Actor = *actor
		}
		if resourceName != nil {
			e.ResourceName = *resourceName
		}
		if requestID != nil {
			e.RequestID = *requestID
		}
		if ipAddress != nil {
			e.IPAddress = *ipAddress
		}
		if statusCode != nil {
			e.StatusCode = *statusCode
		}

		if changesStr != nil && *changesStr != "" {
			var changes Changes
			if err := json.Unmarshal([]byte(*changesStr), &changes); err == nil {
				e.Changes = &changes
			}
		}

		events = append(events, &e)
	}

	return events, rows.Err()
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullBytes(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

func itoa(i int) string {
	digits := ""
	if i == 0 {
		return "0"
	}
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}
