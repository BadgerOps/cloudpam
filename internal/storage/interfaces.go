// Package storage provides storage interfaces and implementations for CloudPAM.
// This file defines extended interfaces for PostgreSQL support and advanced queries.
package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// TransactionalStore extends Store with transaction support.
// This interface is intended for PostgreSQL and other transactional databases.
type TransactionalStore interface {
	Store

	// BeginTx starts a new transaction.
	// The returned Transaction must be either committed or rolled back.
	BeginTx(ctx context.Context) (Transaction, error)

	// WithTx executes a function within a transaction.
	// If the function returns an error, the transaction is rolled back.
	// Otherwise, the transaction is committed.
	WithTx(ctx context.Context, fn func(tx Transaction) error) error
}

// Transaction represents a database transaction.
// It embeds Store to allow all CRUD operations within the transaction scope.
type Transaction interface {
	Store

	// Commit commits the transaction.
	// After Commit, the transaction should not be used.
	Commit() error

	// Rollback aborts the transaction.
	// After Rollback, the transaction should not be used.
	// Rollback is idempotent; calling it on an already committed/rolled back
	// transaction has no effect.
	Rollback() error
}

// Queryable defines common query patterns with flexible filtering.
// This interface provides more advanced query capabilities than the basic Store.
type Queryable interface {
	// QueryPools returns pools matching the given criteria.
	QueryPools(ctx context.Context, opts PoolQueryOptions) ([]*domain.Pool, error)

	// QueryAccounts returns accounts matching the given criteria.
	QueryAccounts(ctx context.Context, opts AccountQueryOptions) ([]*domain.Account, error)

	// CountPools returns the count of pools matching the given criteria.
	CountPools(ctx context.Context, opts PoolQueryOptions) (int64, error)

	// CountAccounts returns the count of accounts matching the given criteria.
	CountAccounts(ctx context.Context, opts AccountQueryOptions) (int64, error)
}

// PoolQueryOptions provides flexible filtering options for pool queries.
type PoolQueryOptions struct {
	// ID filters by exact pool ID.
	ID *int64

	// ParentID filters by parent pool ID.
	// Use zero-value pointer to find root pools (where parent_id IS NULL).
	ParentID *int64

	// AccountID filters by associated account ID.
	// Use zero-value pointer to find unassigned pools (where account_id IS NULL).
	AccountID *int64

	// CIDRPrefix filters pools whose CIDR starts with this prefix.
	// Useful for finding pools in a specific IP range.
	CIDRPrefix string

	// CIDRContains filters pools whose CIDR contains this address/prefix.
	// PostgreSQL-specific: uses the >> (contains) operator.
	CIDRContains string

	// CIDRWithin filters pools whose CIDR is contained within this prefix.
	// PostgreSQL-specific: uses the << (is contained by) operator.
	CIDRWithin string

	// NameContains filters pools whose name contains this substring (case-insensitive).
	NameContains string

	// CreatedAfter filters pools created after this time.
	CreatedAfter *int64 // Unix timestamp

	// CreatedBefore filters pools created before this time.
	CreatedBefore *int64 // Unix timestamp

	// IncludeChildren when true, includes all descendant pools.
	IncludeChildren bool

	// OrderBy specifies the sort field: "id", "name", "cidr", "created_at"
	OrderBy string

	// OrderDesc when true, sorts in descending order.
	OrderDesc bool

	// Limit limits the number of results. 0 means no limit.
	Limit int

	// Offset skips this many results. Used for pagination.
	Offset int
}

// AccountQueryOptions provides flexible filtering options for account queries.
type AccountQueryOptions struct {
	// ID filters by exact account ID.
	ID *int64

	// Key filters by exact account key.
	Key string

	// KeyPrefix filters accounts whose key starts with this prefix.
	KeyPrefix string

	// Provider filters by provider (e.g., "aws", "gcp", "azure").
	Provider string

	// Platform filters by platform.
	Platform string

	// Tier filters by tier.
	Tier string

	// Environment filters by environment (e.g., "prod", "staging", "dev").
	Environment string

	// Region filters accounts that include this region.
	Region string

	// NameContains filters accounts whose name contains this substring (case-insensitive).
	NameContains string

	// CreatedAfter filters accounts created after this time.
	CreatedAfter *int64 // Unix timestamp

	// CreatedBefore filters accounts created before this time.
	CreatedBefore *int64 // Unix timestamp

	// OrderBy specifies the sort field: "id", "key", "name", "created_at"
	OrderBy string

	// OrderDesc when true, sorts in descending order.
	OrderDesc bool

	// Limit limits the number of results. 0 means no limit.
	Limit int

	// Offset skips this many results. Used for pagination.
	Offset int
}

// CIDROperations defines CIDR-specific query operations.
// These are primarily used with PostgreSQL's native CIDR type.
type CIDROperations interface {
	// FindOverlapping returns pools whose CIDR overlaps with the given prefix.
	FindOverlapping(ctx context.Context, cidr string, parentID *int64) ([]*domain.Pool, error)

	// FindContaining returns pools whose CIDR contains the given address or prefix.
	FindContaining(ctx context.Context, cidr string) ([]*domain.Pool, error)

	// FindContainedBy returns pools whose CIDR is contained within the given prefix.
	FindContainedBy(ctx context.Context, cidr string) ([]*domain.Pool, error)

	// FindGaps returns unallocated CIDR ranges within a parent pool.
	FindGaps(ctx context.Context, parentID int64, prefixLen int) ([]string, error)
}

// MigrationStore provides migration management capabilities.
type MigrationStore interface {
	// CurrentVersion returns the current schema version.
	CurrentVersion(ctx context.Context) (int, error)

	// MigrateUp applies all pending migrations.
	MigrateUp(ctx context.Context) error

	// MigrateDown rolls back the last migration.
	MigrateDown(ctx context.Context) error

	// MigrateTo migrates to a specific version (up or down).
	MigrateTo(ctx context.Context, version int) error
}

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

// ExtendedStore combines all extended storage capabilities.
// This is the target interface for PostgreSQL implementation.
type ExtendedStore interface {
	Store
	TransactionalStore
	Queryable
	CIDROperations
	MigrationStore
	HealthCheck
}

// DefaultPoolQueryOptions returns sensible defaults for pool queries.
func DefaultPoolQueryOptions() PoolQueryOptions {
	return PoolQueryOptions{
		OrderBy: "created_at",
		Limit:   50,
		Offset:  0,
	}
}

// DefaultAccountQueryOptions returns sensible defaults for account queries.
func DefaultAccountQueryOptions() AccountQueryOptions {
	return AccountQueryOptions{
		OrderBy: "created_at",
		Limit:   50,
		Offset:  0,
	}
}

// WithLimit returns a copy of options with the specified limit.
func (o PoolQueryOptions) WithLimit(limit int) PoolQueryOptions {
	o.Limit = limit
	return o
}

// WithOffset returns a copy of options with the specified offset.
func (o PoolQueryOptions) WithOffset(offset int) PoolQueryOptions {
	o.Offset = offset
	return o
}

// WithLimit returns a copy of options with the specified limit.
func (o AccountQueryOptions) WithLimit(limit int) AccountQueryOptions {
	o.Limit = limit
	return o
}

// WithOffset returns a copy of options with the specified offset.
func (o AccountQueryOptions) WithOffset(offset int) AccountQueryOptions {
	o.Offset = offset
	return o
}
