//go:build postgres

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// testDB holds a shared database connection for test suites.
// It's initialized once via TestMain and reused across test functions.
var testDB struct {
	connStr   string
	pool      *pgxpool.Pool
	store     *Store
	container testcontainers.Container
}

// TestMain sets up a PostgreSQL database for tests.
// It supports two modes:
//  1. DATABASE_URL env var - uses an existing PostgreSQL instance (CI/custom)
//  2. testcontainers-go - automatically starts a Chainguard PostgreSQL container
func TestMain(m *testing.M) {
	ctx := context.Background()

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		// Start a PostgreSQL container using testcontainers-go
		// Use standard postgres:16-alpine for testcontainers compatibility.
		// Chainguard image is used in docker-compose for local dev.
		container, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("cloudpam_test"),
			tcpostgres.WithUsername("cloudpam"),
			tcpostgres.WithPassword("cloudpam"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start PostgreSQL container: %v\n", err)
			os.Exit(1)
		}
		testDB.container = container

		connStr, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get connection string: %v\n", err)
			_ = container.Terminate(ctx)
			os.Exit(1)
		}
	}

	testDB.connStr = connStr

	// Create the store (runs migrations)
	store, err := New(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create store: %v\n", err)
		if testDB.container != nil {
			_ = testDB.container.Terminate(ctx)
		}
		os.Exit(1)
	}
	testDB.store = store
	testDB.pool = store.Pool()

	code := m.Run()

	// Cleanup
	_ = store.Close()
	if testDB.container != nil {
		_ = testDB.container.Terminate(ctx)
	}

	os.Exit(code)
}

// resetDB truncates all data tables between tests to ensure isolation.
func resetDB(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	// Truncate in dependency order (children before parents)
	tables := []string{
		"pool_utilization_cache",
		"pools",
		"accounts",
		"audit_events",
		"api_tokens",
	}
	for _, table := range tables {
		_, err := testDB.pool.Exec(ctx, "DELETE FROM "+table)
		if err != nil {
			t.Fatalf("failed to reset table %s: %v", table, err)
		}
	}
	// Reset sequences on pools and accounts so seq_id starts at 1
	_, _ = testDB.pool.Exec(ctx, "ALTER SEQUENCE pools_seq_id_seq RESTART WITH 1")
	_, _ = testDB.pool.Exec(ctx, "ALTER SEQUENCE accounts_seq_id_seq RESTART WITH 1")
}

// =============================================================================
// Store: Pool CRUD Tests
// =============================================================================

func TestCreatePool(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("basic pool creation", func(t *testing.T) {
		p, err := s.CreatePool(ctx, domain.CreatePool{
			Name: "test-pool",
			CIDR: "10.0.0.0/8",
		})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}
		if p.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if p.Name != "test-pool" {
			t.Errorf("expected name 'test-pool', got %q", p.Name)
		}
		if p.CIDR != "10.0.0.0/8" {
			t.Errorf("expected CIDR '10.0.0.0/8', got %q", p.CIDR)
		}
		if p.Type != domain.PoolTypeSubnet {
			t.Errorf("expected default type 'subnet', got %q", p.Type)
		}
		if p.Status != domain.PoolStatusActive {
			t.Errorf("expected default status 'active', got %q", p.Status)
		}
		if p.Source != domain.PoolSourceManual {
			t.Errorf("expected default source 'manual', got %q", p.Source)
		}
		if p.CreatedAt.IsZero() {
			t.Error("expected non-zero CreatedAt")
		}
		if p.UpdatedAt.IsZero() {
			t.Error("expected non-zero UpdatedAt")
		}
	})

	t.Run("pool with all fields", func(t *testing.T) {
		resetDB(t)
		p, err := s.CreatePool(ctx, domain.CreatePool{
			Name:        "full-pool",
			CIDR:        "172.16.0.0/12",
			Type:        domain.PoolTypeVPC,
			Status:      domain.PoolStatusPlanned,
			Source:      domain.PoolSourceDiscovered,
			Description: "A fully specified pool",
			Tags:        map[string]string{"env": "prod", "team": "platform"},
		})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}
		if p.Type != domain.PoolTypeVPC {
			t.Errorf("expected type 'vpc', got %q", p.Type)
		}
		if p.Status != domain.PoolStatusPlanned {
			t.Errorf("expected status 'planned', got %q", p.Status)
		}
		if p.Source != domain.PoolSourceDiscovered {
			t.Errorf("expected source 'discovered', got %q", p.Source)
		}
		if p.Description != "A fully specified pool" {
			t.Errorf("unexpected description: %q", p.Description)
		}
		if len(p.Tags) != 2 || p.Tags["env"] != "prod" || p.Tags["team"] != "platform" {
			t.Errorf("unexpected tags: %v", p.Tags)
		}
	})

	t.Run("pool with parent", func(t *testing.T) {
		resetDB(t)
		parent, err := s.CreatePool(ctx, domain.CreatePool{
			Name: "parent",
			CIDR: "10.0.0.0/8",
		})
		if err != nil {
			t.Fatalf("create parent failed: %v", err)
		}

		child, err := s.CreatePool(ctx, domain.CreatePool{
			Name:     "child",
			CIDR:     "10.1.0.0/16",
			ParentID: &parent.ID,
		})
		if err != nil {
			t.Fatalf("create child failed: %v", err)
		}
		if child.ParentID == nil || *child.ParentID != parent.ID {
			t.Errorf("expected parent ID %d, got %v", parent.ID, child.ParentID)
		}
	})

	t.Run("pool with account", func(t *testing.T) {
		resetDB(t)
		acct, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key:  "aws:test-pool-acct",
			Name: "Test Account",
		})
		if err != nil {
			t.Fatalf("create account failed: %v", err)
		}

		p, err := s.CreatePool(ctx, domain.CreatePool{
			Name:      "acct-pool",
			CIDR:      "10.0.0.0/8",
			AccountID: &acct.ID,
		})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}
		if p.AccountID == nil || *p.AccountID != acct.ID {
			t.Errorf("expected account ID %d, got %v", acct.ID, p.AccountID)
		}
	})

	t.Run("validation: empty name", func(t *testing.T) {
		_, err := s.CreatePool(ctx, domain.CreatePool{
			CIDR: "10.0.0.0/8",
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("validation: empty CIDR", func(t *testing.T) {
		_, err := s.CreatePool(ctx, domain.CreatePool{
			Name: "no-cidr",
		})
		if err == nil {
			t.Error("expected error for empty CIDR")
		}
	})

	t.Run("validation: invalid parent ID", func(t *testing.T) {
		resetDB(t)
		badID := int64(999)
		_, err := s.CreatePool(ctx, domain.CreatePool{
			Name:     "orphan",
			CIDR:     "10.0.0.0/8",
			ParentID: &badID,
		})
		if err == nil {
			t.Error("expected error for invalid parent ID")
		}
	})
}

func TestGetPool(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	created, err := s.CreatePool(ctx, domain.CreatePool{
		Name:        "get-pool",
		CIDR:        "192.168.0.0/16",
		Description: "test description",
		Tags:        map[string]string{"key": "value"},
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	t.Run("existing pool", func(t *testing.T) {
		p, found, err := s.GetPool(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetPool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Name != "get-pool" {
			t.Errorf("expected name 'get-pool', got %q", p.Name)
		}
		if p.Description != "test description" {
			t.Errorf("expected description 'test description', got %q", p.Description)
		}
		if p.Tags["key"] != "value" {
			t.Errorf("expected tag key=value, got %v", p.Tags)
		}
	})

	t.Run("non-existent pool", func(t *testing.T) {
		_, found, err := s.GetPool(ctx, 99999)
		if err != nil {
			t.Fatalf("GetPool failed: %v", err)
		}
		if found {
			t.Error("expected pool not to be found")
		}
	})
}

func TestListPools(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("empty list", func(t *testing.T) {
		pools, err := s.ListPools(ctx)
		if err != nil {
			t.Fatalf("ListPools failed: %v", err)
		}
		if len(pools) != 0 {
			t.Errorf("expected 0 pools, got %d", len(pools))
		}
	})

	t.Run("list multiple pools", func(t *testing.T) {
		_, err := s.CreatePool(ctx, domain.CreatePool{Name: "pool-1", CIDR: "10.0.0.0/8"})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}
		_, err = s.CreatePool(ctx, domain.CreatePool{Name: "pool-2", CIDR: "172.16.0.0/12"})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}
		_, err = s.CreatePool(ctx, domain.CreatePool{Name: "pool-3", CIDR: "192.168.0.0/16"})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}

		pools, err := s.ListPools(ctx)
		if err != nil {
			t.Fatalf("ListPools failed: %v", err)
		}
		if len(pools) != 3 {
			t.Errorf("expected 3 pools, got %d", len(pools))
		}

		// Verify ordering by seq_id ASC
		for i := 1; i < len(pools); i++ {
			if pools[i].ID <= pools[i-1].ID {
				t.Errorf("expected ascending order, but pool %d (ID=%d) came after pool %d (ID=%d)",
					i, pools[i].ID, i-1, pools[i-1].ID)
			}
		}
	})
}

func TestUpdatePool(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	created, err := s.CreatePool(ctx, domain.CreatePool{
		Name:        "update-me",
		CIDR:        "10.0.0.0/8",
		Type:        domain.PoolTypeSubnet,
		Description: "original",
		Tags:        map[string]string{"old": "tag"},
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	t.Run("update name", func(t *testing.T) {
		newName := "updated-name"
		p, found, err := s.UpdatePool(ctx, created.ID, domain.UpdatePool{
			Name: &newName,
		})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Name != "updated-name" {
			t.Errorf("expected name 'updated-name', got %q", p.Name)
		}
	})

	t.Run("update type", func(t *testing.T) {
		vpc := domain.PoolTypeVPC
		p, found, err := s.UpdatePool(ctx, created.ID, domain.UpdatePool{
			Type: &vpc,
		})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Type != domain.PoolTypeVPC {
			t.Errorf("expected type 'vpc', got %q", p.Type)
		}
	})

	t.Run("update status", func(t *testing.T) {
		deprecated := domain.PoolStatusDeprecated
		p, found, err := s.UpdatePool(ctx, created.ID, domain.UpdatePool{
			Status: &deprecated,
		})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Status != domain.PoolStatusDeprecated {
			t.Errorf("expected status 'deprecated', got %q", p.Status)
		}
	})

	t.Run("update description", func(t *testing.T) {
		desc := "new description"
		p, found, err := s.UpdatePool(ctx, created.ID, domain.UpdatePool{
			Description: &desc,
		})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Description != "new description" {
			t.Errorf("expected description 'new description', got %q", p.Description)
		}
	})

	t.Run("update tags", func(t *testing.T) {
		newTags := map[string]string{"new": "tag", "env": "staging"}
		p, found, err := s.UpdatePool(ctx, created.ID, domain.UpdatePool{
			Tags: &newTags,
		})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if len(p.Tags) != 2 || p.Tags["new"] != "tag" || p.Tags["env"] != "staging" {
			t.Errorf("unexpected tags: %v", p.Tags)
		}
	})

	t.Run("update non-existent pool", func(t *testing.T) {
		name := "ghost"
		_, found, err := s.UpdatePool(ctx, 99999, domain.UpdatePool{Name: &name})
		if err != nil {
			t.Fatalf("UpdatePool failed: %v", err)
		}
		if found {
			t.Error("expected pool not to be found")
		}
	})
}

func TestUpdatePoolAccount(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	acct, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key:  "aws:pool-acct-test",
		Name: "Pool Account Test",
	})
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	pool, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "pool-for-account",
		CIDR: "10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	t.Run("assign account", func(t *testing.T) {
		p, found, err := s.UpdatePoolAccount(ctx, pool.ID, &acct.ID)
		if err != nil {
			t.Fatalf("UpdatePoolAccount failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.AccountID == nil || *p.AccountID != acct.ID {
			t.Errorf("expected account ID %d, got %v", acct.ID, p.AccountID)
		}
	})

	t.Run("clear account", func(t *testing.T) {
		p, found, err := s.UpdatePoolAccount(ctx, pool.ID, nil)
		if err != nil {
			t.Fatalf("UpdatePoolAccount failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.AccountID != nil {
			t.Errorf("expected nil account ID, got %v", p.AccountID)
		}
	})
}

func TestUpdatePoolMeta(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	pool, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "meta-pool",
		CIDR: "10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	t.Run("update name and account", func(t *testing.T) {
		acct, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key:  "aws:meta-test",
			Name: "Meta Test",
		})
		if err != nil {
			t.Fatalf("CreateAccount failed: %v", err)
		}
		newName := "renamed-pool"
		p, found, err := s.UpdatePoolMeta(ctx, pool.ID, &newName, &acct.ID)
		if err != nil {
			t.Fatalf("UpdatePoolMeta failed: %v", err)
		}
		if !found {
			t.Fatal("expected pool to be found")
		}
		if p.Name != "renamed-pool" {
			t.Errorf("expected name 'renamed-pool', got %q", p.Name)
		}
		if p.AccountID == nil || *p.AccountID != acct.ID {
			t.Errorf("expected account ID %d, got %v", acct.ID, p.AccountID)
		}
	})
}

func TestDeletePool(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("delete leaf pool", func(t *testing.T) {
		p, err := s.CreatePool(ctx, domain.CreatePool{
			Name: "to-delete",
			CIDR: "10.0.0.0/8",
		})
		if err != nil {
			t.Fatalf("CreatePool failed: %v", err)
		}

		deleted, err := s.DeletePool(ctx, p.ID)
		if err != nil {
			t.Fatalf("DeletePool failed: %v", err)
		}
		if !deleted {
			t.Error("expected delete to succeed")
		}

		// Verify soft delete: pool should not be found
		_, found, err := s.GetPool(ctx, p.ID)
		if err != nil {
			t.Fatalf("GetPool failed: %v", err)
		}
		if found {
			t.Error("expected pool to be soft deleted")
		}
	})

	t.Run("delete pool with children fails", func(t *testing.T) {
		resetDB(t)
		parent, err := s.CreatePool(ctx, domain.CreatePool{
			Name: "parent",
			CIDR: "10.0.0.0/8",
		})
		if err != nil {
			t.Fatalf("create parent failed: %v", err)
		}
		_, err = s.CreatePool(ctx, domain.CreatePool{
			Name:     "child",
			CIDR:     "10.1.0.0/16",
			ParentID: &parent.ID,
		})
		if err != nil {
			t.Fatalf("create child failed: %v", err)
		}

		_, err = s.DeletePool(ctx, parent.ID)
		if err == nil {
			t.Error("expected error when deleting pool with children")
		}
	})

	t.Run("delete non-existent pool", func(t *testing.T) {
		deleted, err := s.DeletePool(ctx, 99999)
		if err != nil {
			t.Fatalf("DeletePool failed: %v", err)
		}
		if deleted {
			t.Error("expected delete to return false for non-existent pool")
		}
	})
}

func TestDeletePoolCascade(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Create a 3-level hierarchy: grandparent -> parent -> child
	gp, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "grandparent",
		CIDR: "10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("create grandparent failed: %v", err)
	}

	p, err := s.CreatePool(ctx, domain.CreatePool{
		Name:     "parent",
		CIDR:     "10.1.0.0/16",
		ParentID: &gp.ID,
	})
	if err != nil {
		t.Fatalf("create parent failed: %v", err)
	}

	c, err := s.CreatePool(ctx, domain.CreatePool{
		Name:     "child",
		CIDR:     "10.1.1.0/24",
		ParentID: &p.ID,
	})
	if err != nil {
		t.Fatalf("create child failed: %v", err)
	}

	// Create sibling of parent (should also be deleted)
	sibling, err := s.CreatePool(ctx, domain.CreatePool{
		Name:     "sibling",
		CIDR:     "10.2.0.0/16",
		ParentID: &gp.ID,
	})
	if err != nil {
		t.Fatalf("create sibling failed: %v", err)
	}

	// Cascade delete grandparent
	deleted, err := s.DeletePoolCascade(ctx, gp.ID)
	if err != nil {
		t.Fatalf("DeletePoolCascade failed: %v", err)
	}
	if !deleted {
		t.Error("expected cascade delete to succeed")
	}

	// Verify all 4 pools are gone
	for _, id := range []int64{gp.ID, p.ID, c.ID, sibling.ID} {
		_, found, err := s.GetPool(ctx, id)
		if err != nil {
			t.Fatalf("GetPool failed: %v", err)
		}
		if found {
			t.Errorf("pool %d should have been cascade deleted", id)
		}
	}
}

// =============================================================================
// Store: Pool Hierarchy & Stats Tests
// =============================================================================

func TestGetPoolWithStats(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	parent, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "parent-stats",
		CIDR: "10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	// Create two children: /16 each
	_, err = s.CreatePool(ctx, domain.CreatePool{
		Name: "child-1", CIDR: "10.0.0.0/16", ParentID: &parent.ID,
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}
	_, err = s.CreatePool(ctx, domain.CreatePool{
		Name: "child-2", CIDR: "10.1.0.0/16", ParentID: &parent.ID,
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	pws, err := s.GetPoolWithStats(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetPoolWithStats failed: %v", err)
	}
	if pws == nil {
		t.Fatal("expected non-nil PoolWithStats")
	}
	if pws.Stats.DirectChildren != 2 {
		t.Errorf("expected 2 direct children, got %d", pws.Stats.DirectChildren)
	}
	if pws.Stats.ChildCount != 2 {
		t.Errorf("expected 2 total children, got %d", pws.Stats.ChildCount)
	}
	// /8 = 16,777,216 IPs; two /16 = 2*65,536 = 131,072 used
	if pws.Stats.TotalIPs != 16777216 {
		t.Errorf("expected 16777216 total IPs, got %d", pws.Stats.TotalIPs)
	}
	if pws.Stats.UsedIPs != 131072 {
		t.Errorf("expected 131072 used IPs, got %d", pws.Stats.UsedIPs)
	}

	t.Run("non-existent pool returns error", func(t *testing.T) {
		_, err := s.GetPoolWithStats(ctx, 99999)
		if err == nil {
			t.Error("expected error for non-existent pool")
		}
	})
}

func TestGetPoolHierarchy(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Build a tree:
	// root1 -> child1a, child1b -> grandchild1b
	// root2
	root1, _ := s.CreatePool(ctx, domain.CreatePool{Name: "root1", CIDR: "10.0.0.0/8"})
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "child1a", CIDR: "10.0.0.0/16", ParentID: &root1.ID})
	child1b, _ := s.CreatePool(ctx, domain.CreatePool{Name: "child1b", CIDR: "10.1.0.0/16", ParentID: &root1.ID})
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "grandchild1b", CIDR: "10.1.1.0/24", ParentID: &child1b.ID})
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "root2", CIDR: "172.16.0.0/12"})

	t.Run("full hierarchy", func(t *testing.T) {
		tree, err := s.GetPoolHierarchy(ctx, nil)
		if err != nil {
			t.Fatalf("GetPoolHierarchy failed: %v", err)
		}
		if len(tree) != 2 {
			t.Errorf("expected 2 root nodes, got %d", len(tree))
		}
	})

	t.Run("subtree from root1", func(t *testing.T) {
		tree, err := s.GetPoolHierarchy(ctx, &root1.ID)
		if err != nil {
			t.Fatalf("GetPoolHierarchy failed: %v", err)
		}
		if len(tree) != 1 {
			t.Fatalf("expected 1 root node, got %d", len(tree))
		}
		if tree[0].Name != "root1" {
			t.Errorf("expected root name 'root1', got %q", tree[0].Name)
		}
		if len(tree[0].Children) != 2 {
			t.Errorf("expected 2 children of root1, got %d", len(tree[0].Children))
		}
	})
}

func TestGetPoolChildren(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	parent, _ := s.CreatePool(ctx, domain.CreatePool{Name: "parent", CIDR: "10.0.0.0/8"})
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "child-a", CIDR: "10.0.0.0/16", ParentID: &parent.ID})
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "child-b", CIDR: "10.1.0.0/16", ParentID: &parent.ID})

	children, err := s.GetPoolChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetPoolChildren failed: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}

	t.Run("non-existent parent returns error", func(t *testing.T) {
		_, err := s.GetPoolChildren(ctx, 99999)
		if err == nil {
			t.Error("expected error for non-existent parent")
		}
	})
}

func TestCalculatePoolUtilization(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	parent, _ := s.CreatePool(ctx, domain.CreatePool{Name: "util-parent", CIDR: "10.0.0.0/16"})
	// /16 = 65536; child /24 = 256 used
	_, _ = s.CreatePool(ctx, domain.CreatePool{Name: "util-child", CIDR: "10.0.1.0/24", ParentID: &parent.ID})

	stats, err := s.CalculatePoolUtilization(ctx, parent.ID)
	if err != nil {
		t.Fatalf("CalculatePoolUtilization failed: %v", err)
	}
	if stats.TotalIPs != 65536 {
		t.Errorf("expected 65536 total IPs, got %d", stats.TotalIPs)
	}
	if stats.UsedIPs != 256 {
		t.Errorf("expected 256 used IPs, got %d", stats.UsedIPs)
	}
	if stats.AvailableIPs != 65280 {
		t.Errorf("expected 65280 available IPs, got %d", stats.AvailableIPs)
	}
	// Expected utilization: 256/65536 * 100 ≈ 0.390625
	if stats.Utilization < 0.39 || stats.Utilization > 0.40 {
		t.Errorf("expected utilization ~0.39%%, got %f", stats.Utilization)
	}
}

// =============================================================================
// Store: Account CRUD Tests
// =============================================================================

func TestCreateAccount(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("basic account", func(t *testing.T) {
		a, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key:  "aws:123456789012",
			Name: "Production AWS",
		})
		if err != nil {
			t.Fatalf("CreateAccount failed: %v", err)
		}
		if a.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if a.Key != "aws:123456789012" {
			t.Errorf("expected key 'aws:123456789012', got %q", a.Key)
		}
		if a.Name != "Production AWS" {
			t.Errorf("expected name 'Production AWS', got %q", a.Name)
		}
	})

	t.Run("full account", func(t *testing.T) {
		resetDB(t)
		a, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key:         "gcp:my-project",
			Name:        "GCP Project",
			Provider:    "gcp",
			ExternalID:  "my-project-12345",
			Description: "Main GCP project",
			Platform:    "google-cloud",
			Tier:        "production",
			Environment: "prod",
			Regions:     []string{"us-central1", "us-east1"},
		})
		if err != nil {
			t.Fatalf("CreateAccount failed: %v", err)
		}
		if a.Provider != "gcp" {
			t.Errorf("expected provider 'gcp', got %q", a.Provider)
		}
		if a.ExternalID != "my-project-12345" {
			t.Errorf("expected external ID 'my-project-12345', got %q", a.ExternalID)
		}
		if a.Description != "Main GCP project" {
			t.Errorf("expected description 'Main GCP project', got %q", a.Description)
		}
		if a.Platform != "google-cloud" {
			t.Errorf("expected platform 'google-cloud', got %q", a.Platform)
		}
		if a.Tier != "production" {
			t.Errorf("expected tier 'production', got %q", a.Tier)
		}
		if a.Environment != "prod" {
			t.Errorf("expected environment 'prod', got %q", a.Environment)
		}
		if len(a.Regions) != 2 {
			t.Errorf("expected 2 regions, got %d", len(a.Regions))
		}
	})

	t.Run("validation: empty key", func(t *testing.T) {
		_, err := s.CreateAccount(ctx, domain.CreateAccount{
			Name: "No Key",
		})
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("validation: empty name", func(t *testing.T) {
		_, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key: "test:no-name",
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("duplicate key", func(t *testing.T) {
		resetDB(t)
		_, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key: "aws:duplicate", Name: "First",
		})
		if err != nil {
			t.Fatalf("first CreateAccount failed: %v", err)
		}
		_, err = s.CreateAccount(ctx, domain.CreateAccount{
			Key: "aws:duplicate", Name: "Second",
		})
		if err == nil {
			t.Error("expected error for duplicate key")
		}
	})
}

func TestGetAccount(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	created, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key:      "aws:get-test",
		Name:     "Get Test",
		Provider: "aws",
		Regions:  []string{"us-east-1"},
	})
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	t.Run("existing account", func(t *testing.T) {
		a, found, err := s.GetAccount(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetAccount failed: %v", err)
		}
		if !found {
			t.Fatal("expected account to be found")
		}
		if a.Key != "aws:get-test" {
			t.Errorf("expected key 'aws:get-test', got %q", a.Key)
		}
		if a.Provider != "aws" {
			t.Errorf("expected provider 'aws', got %q", a.Provider)
		}
		if len(a.Regions) != 1 || a.Regions[0] != "us-east-1" {
			t.Errorf("unexpected regions: %v", a.Regions)
		}
	})

	t.Run("non-existent account", func(t *testing.T) {
		_, found, err := s.GetAccount(ctx, 99999)
		if err != nil {
			t.Fatalf("GetAccount failed: %v", err)
		}
		if found {
			t.Error("expected account not to be found")
		}
	})
}

func TestListAccounts(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("empty list", func(t *testing.T) {
		accounts, err := s.ListAccounts(ctx)
		if err != nil {
			t.Fatalf("ListAccounts failed: %v", err)
		}
		if len(accounts) != 0 {
			t.Errorf("expected 0 accounts, got %d", len(accounts))
		}
	})

	t.Run("list multiple", func(t *testing.T) {
		_, _ = s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:1", Name: "AWS 1"})
		_, _ = s.CreateAccount(ctx, domain.CreateAccount{Key: "gcp:1", Name: "GCP 1"})
		_, _ = s.CreateAccount(ctx, domain.CreateAccount{Key: "azure:1", Name: "Azure 1"})

		accounts, err := s.ListAccounts(ctx)
		if err != nil {
			t.Fatalf("ListAccounts failed: %v", err)
		}
		if len(accounts) != 3 {
			t.Errorf("expected 3 accounts, got %d", len(accounts))
		}
	})
}

func TestUpdateAccount(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	created, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key:      "aws:update-test",
		Name:     "Original",
		Provider: "aws",
	})
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	a, found, err := s.UpdateAccount(ctx, created.ID, domain.Account{
		Name:        "Updated Name",
		Provider:    "gcp",
		Description: "updated",
		Regions:     []string{"eu-west-1"},
	})
	if err != nil {
		t.Fatalf("UpdateAccount failed: %v", err)
	}
	if !found {
		t.Fatal("expected account to be found")
	}
	if a.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", a.Name)
	}
	if a.Provider != "gcp" {
		t.Errorf("expected provider 'gcp', got %q", a.Provider)
	}
	if a.Description != "updated" {
		t.Errorf("expected description 'updated', got %q", a.Description)
	}
	if len(a.Regions) != 1 || a.Regions[0] != "eu-west-1" {
		t.Errorf("unexpected regions: %v", a.Regions)
	}

	t.Run("update non-existent account", func(t *testing.T) {
		_, found, err := s.UpdateAccount(ctx, 99999, domain.Account{Name: "Ghost"})
		if err != nil {
			t.Fatalf("UpdateAccount failed: %v", err)
		}
		if found {
			t.Error("expected account not to be found")
		}
	})
}

func TestDeleteAccount(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	t.Run("delete standalone account", func(t *testing.T) {
		a, err := s.CreateAccount(ctx, domain.CreateAccount{
			Key: "aws:delete-me", Name: "Delete Me",
		})
		if err != nil {
			t.Fatalf("CreateAccount failed: %v", err)
		}

		deleted, err := s.DeleteAccount(ctx, a.ID)
		if err != nil {
			t.Fatalf("DeleteAccount failed: %v", err)
		}
		if !deleted {
			t.Error("expected delete to succeed")
		}

		// Verify soft delete
		_, found, err := s.GetAccount(ctx, a.ID)
		if err != nil {
			t.Fatalf("GetAccount failed: %v", err)
		}
		if found {
			t.Error("expected account to be soft deleted")
		}
	})

	t.Run("delete account with pools fails", func(t *testing.T) {
		resetDB(t)
		acct, _ := s.CreateAccount(ctx, domain.CreateAccount{
			Key: "aws:in-use", Name: "In Use",
		})
		_, _ = s.CreatePool(ctx, domain.CreatePool{
			Name: "pool-ref", CIDR: "10.0.0.0/8", AccountID: &acct.ID,
		})

		_, err := s.DeleteAccount(ctx, acct.ID)
		if err == nil {
			t.Error("expected error when deleting account with pools")
		}
	})

	t.Run("delete non-existent account", func(t *testing.T) {
		deleted, err := s.DeleteAccount(ctx, 99999)
		if err != nil {
			t.Fatalf("DeleteAccount failed: %v", err)
		}
		if deleted {
			t.Error("expected delete to return false for non-existent account")
		}
	})
}

func TestDeleteAccountCascade(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	acct, _ := s.CreateAccount(ctx, domain.CreateAccount{
		Key: "aws:cascade-test", Name: "Cascade Test",
	})
	pool1, _ := s.CreatePool(ctx, domain.CreatePool{
		Name: "pool-1", CIDR: "10.0.0.0/16", AccountID: &acct.ID,
	})
	// Child pool of pool1 (should also be cascade deleted)
	pool2, _ := s.CreatePool(ctx, domain.CreatePool{
		Name: "pool-1-child", CIDR: "10.0.1.0/24", ParentID: &pool1.ID,
	})
	// Unrelated pool (should survive)
	unrelated, _ := s.CreatePool(ctx, domain.CreatePool{
		Name: "unrelated", CIDR: "172.16.0.0/12",
	})

	deleted, err := s.DeleteAccountCascade(ctx, acct.ID)
	if err != nil {
		t.Fatalf("DeleteAccountCascade failed: %v", err)
	}
	if !deleted {
		t.Error("expected cascade delete to succeed")
	}

	// Account should be gone
	_, found, _ := s.GetAccount(ctx, acct.ID)
	if found {
		t.Error("expected account to be deleted")
	}

	// Both pools should be gone
	_, found, _ = s.GetPool(ctx, pool1.ID)
	if found {
		t.Error("expected pool1 to be cascade deleted")
	}
	_, found, _ = s.GetPool(ctx, pool2.ID)
	if found {
		t.Error("expected pool2 (child) to be cascade deleted")
	}

	// Unrelated pool should survive
	_, found, _ = s.GetPool(ctx, unrelated.ID)
	if !found {
		t.Error("expected unrelated pool to survive cascade delete")
	}
}

// =============================================================================
// HealthCheck Tests
// =============================================================================

func TestHealthCheck(t *testing.T) {
	ctx := context.Background()
	s := testDB.store

	t.Run("ping succeeds", func(t *testing.T) {
		if err := s.Ping(ctx); err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
	})

	t.Run("stats returns valid data", func(t *testing.T) {
		stats := s.Stats()
		if stats == nil {
			t.Fatal("expected non-nil stats")
		}
		if stats.MaxOpenConnections <= 0 {
			t.Errorf("expected positive max connections, got %d", stats.MaxOpenConnections)
		}
		if stats.OpenConnections < 0 {
			t.Errorf("expected non-negative open connections, got %d", stats.OpenConnections)
		}
	})

	t.Run("implements HealthCheck interface", func(t *testing.T) {
		var _ storage.HealthCheck = s
	})
}

// =============================================================================
// Audit Logger Tests
// =============================================================================

func TestPostgresAuditLogger(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	al := audit.NewPostgresAuditLoggerFromPool(testDB.pool)

	t.Run("log event", func(t *testing.T) {
		event := &audit.AuditEvent{
			// Leave ID empty; the logger generates a UUID
			Timestamp:    time.Now().UTC(),
			Actor:        "test-user",
			ActorType:    audit.ActorTypeAPIKey,
			Action:       audit.ActionCreate,
			ResourceType: audit.ResourcePool,
			ResourceID:   "pool-1",
			ResourceName: "Test Pool",
			StatusCode:   201,
			RequestID:    "req-123",
			IPAddress:    "127.0.0.1",
		}
		if err := al.Log(ctx, event); err != nil {
			t.Fatalf("Log failed: %v", err)
		}
	})

	t.Run("log event with changes", func(t *testing.T) {
		event := &audit.AuditEvent{
			// Leave ID empty; the logger generates a UUID
			Timestamp:    time.Now().UTC(),
			Actor:        "test-user",
			ActorType:    audit.ActorTypeAPIKey,
			Action:       audit.ActionUpdate,
			ResourceType: audit.ResourcePool,
			ResourceID:   "pool-1",
			StatusCode:   200,
			Changes: &audit.Changes{
				Before: map[string]any{"name": "old"},
				After:  map[string]any{"name": "new"},
			},
		}
		if err := al.Log(ctx, event); err != nil {
			t.Fatalf("Log failed: %v", err)
		}
	})

	t.Run("log nil event is no-op", func(t *testing.T) {
		if err := al.Log(ctx, nil); err != nil {
			t.Fatalf("Log(nil) failed: %v", err)
		}
	})

	t.Run("list events", func(t *testing.T) {
		events, total, err := al.List(ctx, audit.ListOptions{Limit: 10})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 2 {
			t.Errorf("expected 2 total events, got %d", total)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events, got %d", len(events))
		}
	})

	t.Run("list with filter by action", func(t *testing.T) {
		events, total, err := al.List(ctx, audit.ListOptions{
			Action: audit.ActionCreate,
			Limit:  10,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 1 {
			t.Errorf("expected 1 create event, got %d", total)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event, got %d", len(events))
		}
	})

	t.Run("list with filter by resource type", func(t *testing.T) {
		events, _, err := al.List(ctx, audit.ListOptions{
			ResourceType: audit.ResourcePool,
			Limit:        10,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 pool events, got %d", len(events))
		}
	})

	t.Run("list with filter by actor", func(t *testing.T) {
		events, _, err := al.List(ctx, audit.ListOptions{
			Actor: "test-user",
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events for test-user, got %d", len(events))
		}
	})

	t.Run("list with time range", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		events, _, err := al.List(ctx, audit.ListOptions{
			Since: &future,
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(events) != 0 {
			t.Errorf("expected 0 future events, got %d", len(events))
		}
	})

	t.Run("list with pagination", func(t *testing.T) {
		events, total, err := al.List(ctx, audit.ListOptions{
			Limit:  1,
			Offset: 0,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if total != 2 {
			t.Errorf("expected 2 total, got %d", total)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event per page, got %d", len(events))
		}
	})

	t.Run("get by resource", func(t *testing.T) {
		events, err := al.GetByResource(ctx, audit.ResourcePool, "pool-1")
		if err != nil {
			t.Fatalf("GetByResource failed: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events for pool-1, got %d", len(events))
		}
	})

	t.Run("get by resource non-existent", func(t *testing.T) {
		events, err := al.GetByResource(ctx, audit.ResourcePool, "nonexistent")
		if err != nil {
			t.Fatalf("GetByResource failed: %v", err)
		}
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("event changes are preserved", func(t *testing.T) {
		events, err := al.GetByResource(ctx, audit.ResourcePool, "pool-1")
		if err != nil {
			t.Fatalf("GetByResource failed: %v", err)
		}
		// Find the update event
		var updateEvent *audit.AuditEvent
		for _, e := range events {
			if e.Action == audit.ActionUpdate {
				updateEvent = e
				break
			}
		}
		if updateEvent == nil {
			t.Fatal("expected to find update event")
		}
		if updateEvent.Changes == nil {
			t.Fatal("expected changes to be present")
		}
		if updateEvent.Changes.Before["name"] != "old" {
			t.Errorf("expected before name 'old', got %v", updateEvent.Changes.Before["name"])
		}
		if updateEvent.Changes.After["name"] != "new" {
			t.Errorf("expected after name 'new', got %v", updateEvent.Changes.After["name"])
		}
	})
}

// =============================================================================
// Key Store Tests
// =============================================================================

func TestPostgresKeyStore(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	ks := auth.NewPostgresKeyStoreFromPool(testDB.pool)

	// Generate a test API key
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"pools:read", "pools:write"},
	})
	if err != nil {
		t.Fatalf("GenerateAPIKey failed: %v", err)
	}
	_ = plaintext // keep for validation tests

	t.Run("create key", func(t *testing.T) {
		if err := ks.Create(ctx, apiKey); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	})

	t.Run("get by prefix", func(t *testing.T) {
		key, err := ks.GetByPrefix(ctx, apiKey.Prefix)
		if err != nil {
			t.Fatalf("GetByPrefix failed: %v", err)
		}
		if key == nil {
			t.Fatal("expected key to be found")
		}
		if key.ID != apiKey.ID {
			t.Errorf("expected ID %q, got %q", apiKey.ID, key.ID)
		}
		if key.Name != "test-key" {
			t.Errorf("expected name 'test-key', got %q", key.Name)
		}
		if len(key.Scopes) != 2 {
			t.Errorf("expected 2 scopes, got %d", len(key.Scopes))
		}
	})

	t.Run("get by prefix not found", func(t *testing.T) {
		key, err := ks.GetByPrefix(ctx, "nonexist")
		if err != nil {
			t.Fatalf("GetByPrefix failed: %v", err)
		}
		if key != nil {
			t.Error("expected nil for non-existent prefix")
		}
	})

	t.Run("get by ID", func(t *testing.T) {
		key, err := ks.GetByID(ctx, apiKey.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if key == nil {
			t.Fatal("expected key to be found")
		}
		if key.Prefix != apiKey.Prefix {
			t.Errorf("expected prefix %q, got %q", apiKey.Prefix, key.Prefix)
		}
	})

	t.Run("get by ID not found", func(t *testing.T) {
		key, err := ks.GetByID(ctx, "00000000-0000-0000-0000-000000000099")
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if key != nil {
			t.Error("expected nil for non-existent ID")
		}
	})

	t.Run("list keys", func(t *testing.T) {
		keys, err := ks.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(keys) != 1 {
			t.Errorf("expected 1 key, got %d", len(keys))
		}
		if keys[0].Name != "test-key" {
			t.Errorf("expected name 'test-key', got %q", keys[0].Name)
		}
	})

	t.Run("update last used", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond) // PG has microsecond precision
		if err := ks.UpdateLastUsed(ctx, apiKey.ID, now); err != nil {
			t.Fatalf("UpdateLastUsed failed: %v", err)
		}
		key, err := ks.GetByID(ctx, apiKey.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if key.LastUsedAt == nil {
			t.Fatal("expected LastUsedAt to be set")
		}
		diff := key.LastUsedAt.Sub(now)
		if diff > time.Millisecond || diff < -time.Millisecond {
			t.Errorf("expected LastUsedAt ~%v, got %v (diff=%v)", now, *key.LastUsedAt, diff)
		}
	})

	t.Run("update last used non-existent", func(t *testing.T) {
		err := ks.UpdateLastUsed(ctx, "00000000-0000-0000-0000-000000000099", time.Now())
		if err == nil {
			t.Error("expected error for non-existent key")
		}
	})

	t.Run("revoke key", func(t *testing.T) {
		if err := ks.Revoke(ctx, apiKey.ID); err != nil {
			t.Fatalf("Revoke failed: %v", err)
		}
		key, err := ks.GetByID(ctx, apiKey.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if !key.Revoked {
			t.Error("expected key to be revoked")
		}
	})

	t.Run("revoke non-existent key", func(t *testing.T) {
		err := ks.Revoke(ctx, "00000000-0000-0000-0000-000000000099")
		if err == nil {
			t.Error("expected error for non-existent key")
		}
	})

	t.Run("revoke already revoked key", func(t *testing.T) {
		// Already revoked above - should fail because WHERE revoked_at IS NULL
		err := ks.Revoke(ctx, apiKey.ID)
		if err == nil {
			t.Error("expected error for already revoked key")
		}
	})

	t.Run("delete key", func(t *testing.T) {
		if err := ks.Delete(ctx, apiKey.ID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		key, err := ks.GetByID(ctx, apiKey.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if key != nil {
			t.Error("expected key to be deleted")
		}
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		err := ks.Delete(ctx, "00000000-0000-0000-0000-000000000099")
		if err == nil {
			t.Error("expected error for non-existent key")
		}
	})

	t.Run("create with nil key", func(t *testing.T) {
		err := ks.Create(ctx, nil)
		if err == nil {
			t.Error("expected error for nil key")
		}
	})

	t.Run("create multiple keys", func(t *testing.T) {
		resetDB(t)
		for i := 0; i < 5; i++ {
			_, key, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
				Name:   fmt.Sprintf("key-%d", i),
				Scopes: []string{"*"},
			})
			if err != nil {
				t.Fatalf("GenerateAPIKey failed: %v", err)
			}
			if err := ks.Create(ctx, key); err != nil {
				t.Fatalf("Create key-%d failed: %v", i, err)
			}
		}
		keys, err := ks.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(keys) != 5 {
			t.Errorf("expected 5 keys, got %d", len(keys))
		}
	})

	t.Run("key with expiration", func(t *testing.T) {
		resetDB(t)
		expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)
		_, key, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
			Name:      "expiring-key",
			Scopes:    []string{"pools:read"},
			ExpiresAt: &expires,
		})
		if err != nil {
			t.Fatalf("GenerateAPIKey failed: %v", err)
		}
		if err := ks.Create(ctx, key); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		stored, err := ks.GetByID(ctx, key.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if stored.ExpiresAt == nil {
			t.Fatal("expected ExpiresAt to be set")
		}
		diff := stored.ExpiresAt.Sub(expires)
		if diff > time.Millisecond || diff < -time.Millisecond {
			t.Errorf("expected ExpiresAt ~%v, got %v", expires, *stored.ExpiresAt)
		}
	})
}

// =============================================================================
// Regression & Edge Case Tests
// =============================================================================

func TestSoftDeleteIsolation(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Create and delete a pool, then create a new one with the same CIDR
	p1, _ := s.CreatePool(ctx, domain.CreatePool{Name: "first", CIDR: "10.0.0.0/8"})
	_, _ = s.DeletePool(ctx, p1.ID)

	// Should succeed because the first pool is soft-deleted
	p2, err := s.CreatePool(ctx, domain.CreatePool{Name: "second", CIDR: "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("CreatePool with same CIDR after soft delete failed: %v", err)
	}
	if p2.ID == p1.ID {
		t.Error("expected different ID for new pool")
	}

	// ListPools should only show the second pool
	pools, _ := s.ListPools(ctx)
	if len(pools) != 1 {
		t.Errorf("expected 1 visible pool, got %d", len(pools))
	}
	if pools[0].ID != p2.ID {
		t.Errorf("expected pool ID %d, got %d", p2.ID, pools[0].ID)
	}
}

func TestSoftDeleteAccountIsolation(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	a1, _ := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:iso", Name: "First"})
	_, _ = s.DeleteAccount(ctx, a1.ID)

	// Creating account with same key should fail due to partial unique index
	// (only unique WHERE deleted_at IS NULL)
	a2, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:iso", Name: "Second"})
	if err != nil {
		t.Fatalf("CreateAccount with same key after soft delete failed: %v", err)
	}
	if a2.ID == a1.ID {
		t.Error("expected different ID for new account")
	}
}

func TestPoolParentChildRelationship(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Create a deep hierarchy and verify traversal
	l0, _ := s.CreatePool(ctx, domain.CreatePool{Name: "L0", CIDR: "10.0.0.0/8"})
	l1, _ := s.CreatePool(ctx, domain.CreatePool{Name: "L1", CIDR: "10.0.0.0/16", ParentID: &l0.ID})
	l2, _ := s.CreatePool(ctx, domain.CreatePool{Name: "L2", CIDR: "10.0.0.0/24", ParentID: &l1.ID})
	l3, _ := s.CreatePool(ctx, domain.CreatePool{Name: "L3", CIDR: "10.0.0.0/28", ParentID: &l2.ID})

	// Verify parent chain
	p, _, _ := s.GetPool(ctx, l3.ID)
	if p.ParentID == nil || *p.ParentID != l2.ID {
		t.Errorf("L3 parent should be L2 (ID=%d), got %v", l2.ID, p.ParentID)
	}

	// Cascade from L1 should delete L1, L2, L3 (but not L0)
	deleted, err := s.DeletePoolCascade(ctx, l1.ID)
	if err != nil {
		t.Fatalf("DeletePoolCascade failed: %v", err)
	}
	if !deleted {
		t.Error("expected cascade delete to succeed")
	}

	_, found, _ := s.GetPool(ctx, l0.ID)
	if !found {
		t.Error("L0 should survive cascade delete of L1")
	}
	for _, id := range []int64{l1.ID, l2.ID, l3.ID} {
		_, found, _ := s.GetPool(ctx, id)
		if found {
			t.Errorf("pool %d should have been cascade deleted", id)
		}
	}
}

func TestConcurrentPoolCreation(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Create many pools concurrently to test connection pool under load
	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			_, err := s.CreatePool(ctx, domain.CreatePool{
				Name: fmt.Sprintf("concurrent-%d", idx),
				CIDR: fmt.Sprintf("10.%d.0.0/16", idx),
			})
			errs <- err
		}(i)
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent create %d failed: %v", i, err)
		}
	}

	pools, err := s.ListPools(ctx)
	if err != nil {
		t.Fatalf("ListPools failed: %v", err)
	}
	if len(pools) != n {
		t.Errorf("expected %d pools, got %d", n, len(pools))
	}
}

func TestEmptyTags(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Pool with nil tags should work
	p, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "no-tags",
		CIDR: "10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("CreatePool failed: %v", err)
	}

	got, found, err := s.GetPool(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPool failed: %v", err)
	}
	if !found {
		t.Fatal("expected pool to be found")
	}
	// Empty tags should be nil or empty map
	if got.Tags != nil && len(got.Tags) != 0 {
		t.Errorf("expected nil or empty tags, got %v", got.Tags)
	}
}

func TestNullableAccountFields(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	// Create account with minimal fields
	a, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key:  "bare:minimal",
		Name: "Minimal Account",
	})
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	got, found, _ := s.GetAccount(ctx, a.ID)
	if !found {
		t.Fatal("expected account to be found")
	}
	// All optional fields should be empty strings (not causing errors)
	if got.Provider != "" {
		t.Errorf("expected empty provider, got %q", got.Provider)
	}
	if got.ExternalID != "" {
		t.Errorf("expected empty external ID, got %q", got.ExternalID)
	}
	if got.Description != "" {
		t.Errorf("expected empty description, got %q", got.Description)
	}
	if got.Regions != nil && len(got.Regions) != 0 {
		t.Errorf("expected nil or empty regions, got %v", got.Regions)
	}
}

func TestMigrationStatus(t *testing.T) {
	status, err := Status(testDB.connStr)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status == "" {
		t.Error("expected non-empty status")
	}
	// Should contain schema_version info
	if !containsSubstring(status, "schema_version=") {
		t.Errorf("expected status to contain 'schema_version=', got: %s", status)
	}
}

func TestStoreClose(t *testing.T) {
	// Create a fresh store and close it
	store, err := New(testDB.connStr)
	if err != nil {
		t.Fatalf("New store failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
