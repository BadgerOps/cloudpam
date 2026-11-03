//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"errors"
)

// withWriteLock runs fn on one connection inside BEGIN IMMEDIATE. SQLite takes the write lock
// before fn's first read, so a delete's liveness check and its write see no other writer's
// commit in between, and a concurrent restore cannot slip a live child under it.
func (s *Store) withWriteLock(ctx context.Context, fn func(c *sql.Conn) error) error {
	c, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err := c.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	if err := fn(c); err != nil {
		_, rbErr := c.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		return errors.Join(err, rbErr)
	}
	if _, err := c.ExecContext(ctx, `COMMIT`); err != nil {
		_, rbErr := c.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		return errors.Join(err, rbErr)
	}
	return nil
}
