//go:build sqlite

package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sqlitestore "cloudpam/internal/storage/sqlite"
)

// The schema declares sessions.user_id ON DELETE CASCADE. It only fires when
// the pool that runs the delete enforces foreign keys.
func TestSQLiteUserStore_DeleteCascadesSessions(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	migrated, err := sqlitestore.New(dsn)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = migrated.Close() })

	users, err := NewSQLiteUserStore(dsn)
	if err != nil {
		t.Fatalf("user store: %v", err)
	}
	t.Cleanup(func() { _ = users.Close() })
	sessions, err := NewSQLiteSessionStore(dsn)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	t.Cleanup(func() { _ = sessions.Close() })

	ctx := context.Background()
	now := time.Now()
	user := &User{ID: "u1", Username: "alice", Email: "alice@example.com", Role: RoleAdmin, PasswordHash: []byte("hash"), IsActive: true, CreatedAt: now, UpdatedAt: now, AuthProvider: "local"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	session := &Session{ID: "s1", UserID: user.ID, Role: RoleAdmin, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if got, err := sessions.Get(ctx, session.ID); err != nil || got == nil {
		t.Fatalf("session missing before delete: session=%v err=%v", got, err)
	}

	if err := users.Delete(ctx, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	got, err := sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("session survived user delete: %+v", got)
	}
}
