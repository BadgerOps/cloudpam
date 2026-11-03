//go:build postgres

package postgres

import (
	"context"
	"testing"

	"cloudpam/internal/auth"
)

func TestPostgresKeyStoreRateLimit(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	ks := auth.NewPostgresKeyStoreFromPool(testDB.pool)

	want := auth.RateLimit{RequestsPerSecond: 2.5, Burst: 7}
	_, limited, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: "limited", RateLimit: &want})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	_, plain, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: "plain"})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	for _, k := range []*auth.APIKey{limited, plain} {
		if err := ks.Create(ctx, k); err != nil {
			t.Fatalf("Create %s: %v", k.Name, err)
		}
	}

	byPrefix, err := ks.GetByPrefix(ctx, limited.Prefix)
	if err != nil || byPrefix == nil {
		t.Fatalf("GetByPrefix: key=%v err=%v", byPrefix, err)
	}
	if byPrefix.RateLimit == nil || *byPrefix.RateLimit != want {
		t.Errorf("GetByPrefix rate limit = %v, want %v", byPrefix.RateLimit, want)
	}
	listed, err := ks.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, k := range listed {
		switch k.ID {
		case limited.ID:
			if k.RateLimit == nil || *k.RateLimit != want {
				t.Errorf("List rate limit for limited key = %v, want %v", k.RateLimit, want)
			}
		case plain.ID:
			if k.RateLimit != nil {
				t.Errorf("List rate limit for plain key = %v, want nil", k.RateLimit)
			}
		}
	}
}
