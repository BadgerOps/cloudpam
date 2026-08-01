//go:build !sqlite && !postgres

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func selectorLogger(buf *bytes.Buffer) observability.Logger {
	return observability.NewLogger(observability.Config{Level: "debug", Format: "json", Output: buf})
}

// TestRequireMemoryModeOptIns documents every env var that unlocks the
// in-memory storage backend. If one of these stops working the server silently
// refuses to boot in dev.
func TestRequireMemoryModeOptIns(t *testing.T) {
	optIns := []struct {
		key   string
		value string
	}{
		{key: "DEV_MODE", value: "1"},
		{key: "CLOUDPAM_DEV_MODE", value: "1"},
		{key: "CLOUDPAM_STORAGE", value: "memory"},
	}
	for _, tc := range optIns {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("DEV_MODE", "")
			t.Setenv("CLOUDPAM_DEV_MODE", "")
			t.Setenv("CLOUDPAM_STORAGE", "")
			t.Setenv(tc.key, tc.value)

			// requireMemoryMode calls os.Exit on rejection, so simply returning
			// proves the opt-in was honoured.
			requireMemoryMode(discardLogger())
		})
	}
}

func TestDefaultSelectorsReturnUsableMemoryStores(t *testing.T) {
	t.Setenv("DEV_MODE", "1")
	ctx := context.Background()
	logger := discardLogger()

	if auditLogger := selectAuditLogger(logger); auditLogger == nil {
		t.Fatal("selectAuditLogger returned nil")
	}

	keyStore := selectKeyStore(logger)
	_, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: "selector", Scopes: []string{"pools:read"}})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := keyStore.Create(ctx, apiKey); err != nil {
		t.Fatalf("keyStore.Create: %v", err)
	}
	got, err := keyStore.GetByPrefix(ctx, apiKey.Prefix)
	if err != nil {
		t.Fatalf("keyStore.GetByPrefix: %v", err)
	}
	if got == nil || got.ID != apiKey.ID {
		t.Fatalf("keyStore round trip returned %v, want key %s", got, apiKey.ID)
	}

	userStore := selectUserStore(logger)
	hash, err := auth.HashPassword("SelectorPass123!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &auth.User{
		ID:           uuid.NewString(),
		Username:     "selector",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("userStore.Create: %v", err)
	}

	// The role store must be bound to the same user store it was given,
	// otherwise role lookups would silently miss every user.
	roleStore := selectRoleStore(logger, userStore)
	if roleStore == nil {
		t.Fatal("selectRoleStore returned nil")
	}

	sessionStore := selectSessionStore(logger)
	session, err := auth.NewSession(user.ID, user.Role, time.Hour, nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sessionStore.Create(ctx, session); err != nil {
		t.Fatalf("sessionStore.Create: %v", err)
	}
	sessions, err := sessionStore.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("sessionStore.ListByUserID: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessionStore returned %d sessions, want 1", len(sessions))
	}
}

func TestDefaultDerivedStoresAreUsable(t *testing.T) {
	t.Setenv("DEV_MODE", "1")
	ctx := context.Background()
	logger := discardLogger()
	main := storage.NewMemoryStore()
	t.Cleanup(func() { _ = main.Close() })

	discoveryStore := selectDiscoveryStore(logger, main)
	res := domain.DiscoveredResource{
		ID:           uuid.New(),
		AccountID:    1,
		Provider:     "aws",
		ResourceID:   "vpc-1",
		ResourceType: domain.ResourceTypeVPC,
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: time.Now().UTC(),
		LastSeenAt:   time.Now().UTC(),
	}
	if err := discoveryStore.UpsertDiscoveredResource(ctx, res); err != nil {
		t.Fatalf("UpsertDiscoveredResource: %v", err)
	}
	list, total, err := discoveryStore.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("discovery store round trip returned total=%d len=%d, want 1/1", total, len(list))
	}

	if got := selectRecommendationStore(logger, main); got == nil {
		t.Error("selectRecommendationStore returned nil")
	}
	if got := selectConversationStore(logger, main); got == nil {
		t.Error("selectConversationStore returned nil")
	}
	if got := selectDriftStore(logger, main); got == nil {
		t.Error("selectDriftStore returned nil")
	}
	if got := selectNetworkStore(logger, main); got == nil {
		t.Error("selectNetworkStore returned nil")
	}
	if got := selectSettingsStore(logger, main); got == nil {
		t.Error("selectSettingsStore returned nil")
	}
	if got := selectOIDCProviderStore(logger, main); got == nil {
		t.Error("selectOIDCProviderStore returned nil")
	}
}

// TestMigrationStatusUnavailableInMemoryBuild asserts the no-tag binary reports
// that migrations are not applicable rather than pretending to have a schema.
func TestMigrationStatusUnavailableInMemoryBuild(t *testing.T) {
	t.Setenv("DEV_MODE", "1")
	t.Setenv("SQLITE_DSN", "")
	t.Setenv("DATABASE_URL", "")

	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "status", cmd: "status", want: "migrations status not available in this build"},
		{name: "up falls through to status", cmd: "up", want: "migrations status not available in this build"},
		{name: "unknown command warns", cmd: "sideways", want: "unknown migrate command"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			runMigrationsCLI(selectorLogger(&logs), tc.cmd)
			if !strings.Contains(logs.String(), tc.want) {
				t.Errorf("runMigrationsCLI(%q) logged %s, want it to contain %q", tc.cmd, logs.String(), tc.want)
			}
		})
	}
}

func TestSqliteAndPostgresStatusAreEmptyInMemoryBuild(t *testing.T) {
	if got := sqliteStatus("file:ignored.db"); got != "" {
		t.Errorf("sqliteStatus() = %q, want empty on a no-tag build", got)
	}
	if got := postgresStatus(); got != "" {
		t.Errorf("postgresStatus() = %q, want empty on a no-tag build", got)
	}
}
