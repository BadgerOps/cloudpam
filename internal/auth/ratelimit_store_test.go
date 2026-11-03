package auth

import (
	"context"
	"testing"
)

// testKeyStoreRateLimit checks that a store returns a key's optional rate
// limit from every read path, and no limit for a key created without one.
func testKeyStoreRateLimit(t *testing.T, store KeyStore) {
	t.Helper()
	ctx := context.Background()

	_, limited, err := GenerateAPIKey(GenerateAPIKeyOptions{
		Name:      "limited",
		Scopes:    []string{"pools:read"},
		RateLimit: &RateLimit{RequestsPerSecond: 2.5, Burst: 7},
	})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	_, plain, err := GenerateAPIKey(GenerateAPIKeyOptions{Name: "plain", Scopes: []string{"pools:read"}})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	for _, k := range []*APIKey{limited, plain} {
		if err := store.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.Name, err)
		}
	}

	want := RateLimit{RequestsPerSecond: 2.5, Burst: 7}
	byPrefix, err := store.GetByPrefix(ctx, limited.Prefix)
	if err != nil || byPrefix == nil {
		t.Fatalf("GetByPrefix: key=%v err=%v", byPrefix, err)
	}
	if byPrefix.RateLimit == nil || *byPrefix.RateLimit != want {
		t.Errorf("GetByPrefix rate limit = %v, want %v", byPrefix.RateLimit, want)
	}
	byID, err := store.GetByID(ctx, limited.ID)
	if err != nil || byID == nil {
		t.Fatalf("GetByID: key=%v err=%v", byID, err)
	}
	if byID.RateLimit == nil || *byID.RateLimit != want {
		t.Errorf("GetByID rate limit = %v, want %v", byID.RateLimit, want)
	}

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := map[string]*RateLimit{}
	for _, k := range listed {
		found[k.ID] = k.RateLimit
	}
	if got := found[limited.ID]; got == nil || *got != want {
		t.Errorf("List rate limit for limited key = %v, want %v", got, want)
	}
	if got := found[plain.ID]; got != nil {
		t.Errorf("List rate limit for plain key = %v, want nil", got)
	}
	unlimited, err := store.GetByPrefix(ctx, plain.Prefix)
	if err != nil || unlimited == nil {
		t.Fatalf("GetByPrefix plain: key=%v err=%v", unlimited, err)
	}
	if unlimited.RateLimit != nil {
		t.Errorf("plain key rate limit = %v, want nil", unlimited.RateLimit)
	}
}

func TestMemoryKeyStoreRateLimit(t *testing.T) {
	store := NewMemoryKeyStore()
	testKeyStoreRateLimit(t, store)

	// A caller that edits a returned key must not change what the store holds.
	keys, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, k := range keys {
		if k.RateLimit != nil {
			k.RateLimit.Burst = 1
			again, _ := store.GetByID(context.Background(), k.ID)
			if again.RateLimit.Burst != 7 {
				t.Fatalf("stored burst changed through a returned key: %d", again.RateLimit.Burst)
			}
		}
	}
}
