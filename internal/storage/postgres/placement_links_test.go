//go:build postgres

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// With one connection in the pool, the placement transaction holds it, so link reads and writes
// made under the lock must run on that transaction or wait out the deadline.
func TestPlacementLinksUseTheLockedConnection(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(testDB.connStr)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	st := NewFromPool(pool)

	account, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "aws:links", Name: "links", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	from, err := st.CreatePool(ctx, domain.CreatePool{Name: "from", CIDR: "10.80.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	to, err := st.CreatePool(ctx, domain.CreatePool{Name: "to", CIDR: "10.81.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	resID := uuid.New()
	if err := st.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{ID: resID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-links", CIDR: "10.80.0.0/24", PoolID: &from.ID, Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}

	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rollback := errors.New("roll back")
	err = st.WithPlacementLock(short, func(ps storage.PlacementStore) error {
		if _, found, err := ps.GetPool(short, to.ID); err != nil || !found {
			return errors.Join(errors.New("get pool under the lock"), err)
		}
		links := storage.LinksIn(ps, st)
		ids, err := links.PoolResourceIDs(short, from.ID)
		if err != nil || !slices.Equal(ids, []uuid.UUID{resID}) {
			return errors.Join(errors.New("pool resource ids under the lock"), err)
		}
		if err := links.LinkResourceToPool(short, resID, to.ID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("WithPlacementLock = %v, want the callback's error", err)
	}
	if ids, err := st.PoolResourceIDs(ctx, from.ID); err != nil || !slices.Equal(ids, []uuid.UUID{resID}) {
		t.Fatalf("after a rolled-back placement the resource links %v err=%v, want still on pool %d", ids, err, from.ID)
	}
}
