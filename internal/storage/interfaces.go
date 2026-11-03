package storage

import "context"

// HealthCheck provides database health checking.
type HealthCheck interface {
	// Ping checks database connectivity.
	Ping(ctx context.Context) error

	// Stats returns database connection pool statistics.
	Stats() *DBStats
}

// DBStats contains database connection pool statistics.
type DBStats struct {
	// MaxOpenConnections is the maximum number of open connections.
	MaxOpenConnections int

	// OpenConnections is the current number of open connections.
	OpenConnections int

	// InUse is the number of connections currently in use.
	InUse int

	// Idle is the number of idle connections.
	Idle int

	// WaitCount is the total number of connections waited for.
	WaitCount int64

	// WaitDuration is the total time blocked waiting for a new connection.
	WaitDuration int64 // nanoseconds
}
