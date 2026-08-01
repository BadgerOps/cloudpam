package storage

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
)

func seedSearchStoreExtra(t *testing.T) *MemoryStore {
	t.Helper()
	ctx := context.Background()
	s := NewMemoryStore()

	supernet, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "Prod Supernet", CIDR: "10.0.0.0/8",
		Type: domain.PoolTypeSupernet, Status: domain.PoolStatusActive,
		Description: "top level range",
	})
	if err != nil {
		t.Fatalf("CreatePool(supernet): %v", err)
	}
	if _, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "Prod VPC", CIDR: "10.1.0.0/16", ParentID: &supernet.ID,
		Type: domain.PoolTypeVPC, Status: domain.PoolStatusActive,
	}); err != nil {
		t.Fatalf("CreatePool(prod vpc): %v", err)
	}
	if _, err := s.CreatePool(ctx, domain.CreatePool{
		Name: "Dev VPC", CIDR: "192.168.0.0/16",
		Type: domain.PoolTypeVPC, Status: domain.PoolStatusPlanned,
		Description: "development sandbox",
	}); err != nil {
		t.Fatalf("CreatePool(dev vpc): %v", err)
	}
	// A pool whose CIDR cannot be parsed must never match a CIDR filter.
	if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "Broken Pool", CIDR: "not-a-cidr"}); err != nil {
		t.Fatalf("CreatePool(broken): %v", err)
	}

	if _, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key: "aws:111", Name: "Prod Account", Provider: "aws", Description: "production billing",
	}); err != nil {
		t.Fatalf("CreateAccount(prod): %v", err)
	}
	if _, err := s.CreateAccount(ctx, domain.CreateAccount{
		Key: "gcp:222", Name: "Dev Project", Provider: "gcp",
	}); err != nil {
		t.Fatalf("CreateAccount(dev): %v", err)
	}
	return s
}

func searchNamesExtra(items []domain.SearchResultItem) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[it.Type+":"+it.Name] = true
	}
	return out
}

func TestMemoryStoreExtra_SearchInvalidCIDRFilters(t *testing.T) {
	ctx := context.Background()
	s := seedSearchStoreExtra(t)

	tests := []struct {
		name string
		req  domain.SearchRequest
	}{
		{"unparsable cidr_contains", domain.SearchRequest{CIDRContains: "not-a-cidr"}},
		{"ipv6 cidr_contains", domain.SearchRequest{CIDRContains: "2001:db8::/32"}},
		{"unparsable cidr_within", domain.SearchRequest{CIDRWithin: "10.0.0.0/99"}},
		{"ipv6 cidr_within", domain.SearchRequest{CIDRWithin: "::1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.Search(ctx, tt.req)
			if err == nil {
				t.Fatalf("Search = %+v, want an error", resp)
			}
			if resp.Total != 0 || len(resp.Items) != 0 {
				t.Errorf("failed search returned data: %+v", resp)
			}
		})
	}
}

func TestMemoryStoreExtra_SearchFilters(t *testing.T) {
	ctx := context.Background()
	s := seedSearchStoreExtra(t)

	tests := []struct {
		name string
		req  domain.SearchRequest
		want []string
	}{
		{
			"empty request returns pools and accounts",
			domain.SearchRequest{},
			[]string{"pool:Prod Supernet", "pool:Prod VPC", "pool:Dev VPC", "pool:Broken Pool", "account:Prod Account", "account:Dev Project"},
		},
		{
			"type filter pool only",
			domain.SearchRequest{Types: []string{"pool"}},
			[]string{"pool:Prod Supernet", "pool:Prod VPC", "pool:Dev VPC", "pool:Broken Pool"},
		},
		{
			"type filter account only",
			domain.SearchRequest{Types: []string{"account"}},
			[]string{"account:Prod Account", "account:Dev Project"},
		},
		{
			"unknown type matches nothing",
			domain.SearchRequest{Types: []string{"widget"}},
			nil,
		},
		{
			"cidr_contains with a bare IP matches enclosing pools only",
			domain.SearchRequest{CIDRContains: "10.1.2.5"},
			[]string{"pool:Prod Supernet", "pool:Prod VPC"},
		},
		{
			"cidr_contains with a prefix",
			domain.SearchRequest{CIDRContains: "10.1.0.0/16"},
			[]string{"pool:Prod Supernet", "pool:Prod VPC"},
		},
		{
			"cidr_within finds pools inside the given prefix",
			domain.SearchRequest{CIDRWithin: "10.0.0.0/8"},
			[]string{"pool:Prod Supernet", "pool:Prod VPC"},
		},
		{
			"cidr filters combined",
			domain.SearchRequest{CIDRContains: "10.1.2.5", CIDRWithin: "10.1.0.0/16"},
			[]string{"pool:Prod VPC"},
		},
		{
			"query matches pool name case-insensitively",
			domain.SearchRequest{Query: "PROD", Types: []string{"pool"}},
			[]string{"pool:Prod Supernet", "pool:Prod VPC"},
		},
		{
			"query matches pool cidr",
			domain.SearchRequest{Query: "192.168", Types: []string{"pool"}},
			[]string{"pool:Dev VPC"},
		},
		{
			"query matches pool description",
			domain.SearchRequest{Query: "sandbox", Types: []string{"pool"}},
			[]string{"pool:Dev VPC"},
		},
		{
			"query matches pool type",
			domain.SearchRequest{Query: "supernet", Types: []string{"pool"}},
			[]string{"pool:Prod Supernet"},
		},
		{
			"query matches pool status",
			domain.SearchRequest{Query: "planned", Types: []string{"pool"}},
			[]string{"pool:Dev VPC"},
		},
		{
			"query matches account name",
			domain.SearchRequest{Query: "dev project", Types: []string{"account"}},
			[]string{"account:Dev Project"},
		},
		{
			"query matches account key",
			domain.SearchRequest{Query: "aws:111", Types: []string{"account"}},
			[]string{"account:Prod Account"},
		},
		{
			"query matches account description",
			domain.SearchRequest{Query: "billing", Types: []string{"account"}},
			[]string{"account:Prod Account"},
		},
		{
			"query matches account provider",
			domain.SearchRequest{Query: "gcp", Types: []string{"account"}},
			[]string{"account:Dev Project"},
		},
		{
			"query with no match",
			domain.SearchRequest{Query: "zzzz"},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.Search(ctx, tt.req)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if resp.Total != len(tt.want) {
				t.Fatalf("Total = %d, want %d (items %+v)", resp.Total, len(tt.want), resp.Items)
			}
			got := searchNamesExtra(resp.Items)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for _, want := range tt.want {
				if !got[want] {
					t.Errorf("missing %q in %v", want, got)
				}
			}
		})
	}
}

func TestMemoryStoreExtra_SearchPopulatesResultFields(t *testing.T) {
	ctx := context.Background()
	s := seedSearchStoreExtra(t)

	resp, err := s.Search(ctx, domain.SearchRequest{Query: "Prod VPC", Types: []string{"pool"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
	}
	item := resp.Items[0]
	if item.Type != "pool" || item.CIDR != "10.1.0.0/16" || item.PoolType != string(domain.PoolTypeVPC) ||
		item.Status != string(domain.PoolStatusActive) {
		t.Errorf("pool result = %+v, want the Prod VPC pool projection", item)
	}
	if item.ParentID == nil {
		t.Error("ParentID = nil, want the supernet ID")
	}

	resp, err = s.Search(ctx, domain.SearchRequest{Query: "Prod Account", Types: []string{"account"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(resp.Items))
	}
	acct := resp.Items[0]
	if acct.Type != "account" || acct.AccountKey != "aws:111" || acct.Provider != "aws" ||
		acct.Description != "production billing" {
		t.Errorf("account result = %+v, want the Prod Account projection", acct)
	}
}

func TestMemoryStoreExtra_SearchPagination(t *testing.T) {
	ctx := context.Background()
	s := seedSearchStoreExtra(t)

	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
		wantLen      int
	}{
		{"defaults", 0, 0, 1, 50, 6},
		{"negative page and size fall back to defaults", -3, -7, 1, 50, 6},
		{"first page of two", 1, 4, 1, 4, 4},
		{"partial last page", 2, 4, 2, 4, 2},
		{"page past the end", 99, 4, 99, 4, 0},
		{"page size clamped to 200", 1, 5000, 1, 200, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.Search(ctx, domain.SearchRequest{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if resp.Total != 6 {
				t.Errorf("Total = %d, want 6 (total must ignore pagination)", resp.Total)
			}
			if resp.Page != tt.wantPage {
				t.Errorf("Page = %d, want %d", resp.Page, tt.wantPage)
			}
			if resp.PageSize != tt.wantPageSize {
				t.Errorf("PageSize = %d, want %d", resp.PageSize, tt.wantPageSize)
			}
			if len(resp.Items) != tt.wantLen {
				t.Errorf("len(Items) = %d, want %d", len(resp.Items), tt.wantLen)
			}
		})
	}
}

func TestMemoryStoreExtra_SearchSkipsSoftDeletedRows(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "Doomed Pool", CIDR: "10.9.0.0/16"})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	acct, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:doomed", Name: "Doomed Account"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	resp, err := s.Search(ctx, domain.SearchRequest{Query: "doomed"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("Total = %d, want 2 before deletion", resp.Total)
	}

	if ok, err := s.DeletePool(ctx, pool.ID); err != nil || !ok {
		t.Fatalf("DeletePool = (%v, %v)", ok, err)
	}
	if ok, err := s.DeleteAccount(ctx, acct.ID); err != nil || !ok {
		t.Fatalf("DeleteAccount = (%v, %v)", ok, err)
	}

	resp, err = s.Search(ctx, domain.SearchRequest{Query: "doomed"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Total != 0 || len(resp.Items) != 0 {
		t.Errorf("soft-deleted rows are still searchable: %+v", resp)
	}

	// CIDR search must skip them too.
	resp, err = s.Search(ctx, domain.SearchRequest{CIDRContains: "10.9.0.1"})
	if err != nil {
		t.Fatalf("Search(cidr): %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("soft-deleted pool matched a CIDR search: %+v", resp)
	}
}

func TestMemoryStoreExtra_GetAccountByKey(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	created, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123456789012", Name: "Prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if _, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "gcp:other", Name: "Other"}); err != nil {
		t.Fatalf("CreateAccount(other): %v", err)
	}

	got, err := s.GetAccountByKey(ctx, "aws:123456789012")
	if err != nil {
		t.Fatalf("GetAccountByKey: %v", err)
	}
	if got.ID != created.ID || got.Name != "Prod" {
		t.Errorf("GetAccountByKey = %+v, want %+v", got, created)
	}

	if _, err := s.GetAccountByKey(ctx, "aws:does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccountByKey(missing) error = %v, want ErrNotFound", err)
	}

	// Key lookup is exact, not a prefix or substring match.
	if _, err := s.GetAccountByKey(ctx, "aws:1234"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccountByKey(partial key) error = %v, want ErrNotFound", err)
	}

	if ok, err := s.DeleteAccount(ctx, created.ID); err != nil || !ok {
		t.Fatalf("DeleteAccount = (%v, %v)", ok, err)
	}
	if _, err := s.GetAccountByKey(ctx, "aws:123456789012"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccountByKey(soft-deleted) error = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreExtra_SoftDeletedRowsAreHiddenAndNotRedeletable(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "p", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	acct, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "k", Name: "a"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if ok, err := s.DeletePool(ctx, pool.ID); err != nil || !ok {
		t.Fatalf("DeletePool = (%v, %v)", ok, err)
	}
	if ok, err := s.DeleteAccount(ctx, acct.ID); err != nil || !ok {
		t.Fatalf("DeleteAccount = (%v, %v)", ok, err)
	}

	// Deleting again is a no-op, not an error and not a second success.
	if ok, err := s.DeletePool(ctx, pool.ID); err != nil || ok {
		t.Errorf("second DeletePool = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := s.DeletePoolCascade(ctx, pool.ID); err != nil || ok {
		t.Errorf("DeletePoolCascade on deleted pool = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := s.DeleteAccount(ctx, acct.ID); err != nil || ok {
		t.Errorf("second DeleteAccount = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := s.DeleteAccountCascade(ctx, acct.ID); err != nil || ok {
		t.Errorf("DeleteAccountCascade on deleted account = (%v, %v), want (false, nil)", ok, err)
	}

	if pools, err := s.ListPools(ctx); err != nil || len(pools) != 0 {
		t.Errorf("ListPools = (%d, %v), want (0, nil)", len(pools), err)
	}
	if accounts, err := s.ListAccounts(ctx); err != nil || len(accounts) != 0 {
		t.Errorf("ListAccounts = (%d, %v), want (0, nil)", len(accounts), err)
	}
	if _, ok, err := s.GetPool(ctx, pool.ID); err != nil || ok {
		t.Errorf("GetPool(deleted) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if _, ok, err := s.GetAccount(ctx, acct.ID); err != nil || ok {
		t.Errorf("GetAccount(deleted) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if _, err := s.GetPoolWithStats(ctx, pool.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPoolWithStats(deleted) error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetPoolChildren(ctx, pool.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPoolChildren(deleted) error = %v, want ErrNotFound", err)
	}
	if _, err := s.CalculatePoolUtilization(ctx, pool.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("CalculatePoolUtilization(deleted) error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetPoolHierarchy(ctx, &pool.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPoolHierarchy(deleted root) error = %v, want ErrNotFound", err)
	}
	if roots, err := s.GetPoolHierarchy(ctx, nil); err != nil || len(roots) != 0 {
		t.Errorf("GetPoolHierarchy(nil) = (%d roots, %v), want (0, nil)", len(roots), err)
	}
}

func TestMemoryStoreExtra_UpdatePoolClearsTags(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "p", CIDR: "10.0.0.0/16", Tags: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if pool.Tags["env"] != "prod" {
		t.Fatalf("Tags = %v, want {env:prod}", pool.Tags)
	}

	var nilTags map[string]string
	updated, ok, err := s.UpdatePool(ctx, pool.ID, domain.UpdatePool{Tags: &nilTags})
	if err != nil || !ok {
		t.Fatalf("UpdatePool = (%v, %v)", ok, err)
	}
	if updated.Tags != nil {
		t.Errorf("Tags = %v, want nil after an explicit nil-map update", updated.Tags)
	}

	got, ok, err := s.GetPool(ctx, pool.ID)
	if err != nil || !ok {
		t.Fatalf("GetPool = (%v, %v)", ok, err)
	}
	if got.Tags != nil {
		t.Errorf("stored Tags = %v, want nil", got.Tags)
	}
}

func TestMemoryStoreExtra_PoolStatsEdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("unparsable pool cidr yields zero stats", func(t *testing.T) {
		s := NewMemoryStore()
		pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "bad", CIDR: "not-a-cidr"})
		if err != nil {
			t.Fatalf("CreatePool: %v", err)
		}
		stats, err := s.CalculatePoolUtilization(ctx, pool.ID)
		if err != nil {
			t.Fatalf("CalculatePoolUtilization: %v", err)
		}
		if (*stats != domain.PoolStats{}) {
			t.Errorf("stats = %+v, want the zero value", *stats)
		}
	})

	t.Run("ipv6 pool sizes are capped", func(t *testing.T) {
		s := NewMemoryStore()
		huge, err := s.CreatePool(ctx, domain.CreatePool{Name: "v6-huge", CIDR: "2001:db8::/32"})
		if err != nil {
			t.Fatalf("CreatePool: %v", err)
		}
		stats, err := s.CalculatePoolUtilization(ctx, huge.ID)
		if err != nil {
			t.Fatalf("CalculatePoolUtilization: %v", err)
		}
		if stats.TotalIPs != int64(1)<<62 {
			t.Errorf("TotalIPs = %d, want the 1<<62 cap", stats.TotalIPs)
		}

		small, err := s.CreatePool(ctx, domain.CreatePool{Name: "v6-small", CIDR: "2001:db8::/100"})
		if err != nil {
			t.Fatalf("CreatePool: %v", err)
		}
		stats, err = s.CalculatePoolUtilization(ctx, small.ID)
		if err != nil {
			t.Fatalf("CalculatePoolUtilization: %v", err)
		}
		if stats.TotalIPs != int64(1)<<28 {
			t.Errorf("TotalIPs = %d, want %d", stats.TotalIPs, int64(1)<<28)
		}
	})

	t.Run("children with unparsable cidrs are counted but contribute no used ips", func(t *testing.T) {
		s := NewMemoryStore()
		parent, err := s.CreatePool(ctx, domain.CreatePool{Name: "parent", CIDR: "10.0.0.0/16"})
		if err != nil {
			t.Fatalf("CreatePool: %v", err)
		}
		for _, child := range []domain.CreatePool{
			{Name: "good", CIDR: "10.0.1.0/24", ParentID: &parent.ID},
			{Name: "bad", CIDR: "garbage", ParentID: &parent.ID},
			{Name: "v6", CIDR: "2001:db8::/64", ParentID: &parent.ID},
		} {
			if _, err := s.CreatePool(ctx, child); err != nil {
				t.Fatalf("CreatePool(%s): %v", child.Name, err)
			}
		}

		stats, err := s.CalculatePoolUtilization(ctx, parent.ID)
		if err != nil {
			t.Fatalf("CalculatePoolUtilization: %v", err)
		}
		if stats.DirectChildren != 3 {
			t.Errorf("DirectChildren = %d, want 3", stats.DirectChildren)
		}
		if stats.ChildCount != 3 {
			t.Errorf("ChildCount = %d, want 3", stats.ChildCount)
		}
		if stats.UsedIPs != 256 {
			t.Errorf("UsedIPs = %d, want 256 (only the parsable IPv4 child counts)", stats.UsedIPs)
		}
		if stats.AvailableIPs != 65536-256 {
			t.Errorf("AvailableIPs = %d, want %d", stats.AvailableIPs, 65536-256)
		}
	})
}
