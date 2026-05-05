package main

import (
	"context"
	"testing"
	"time"

	"cloudpam/internal/auth"
)

func TestResetUserPassword(t *testing.T) {
	ctx := context.Background()
	userStore := auth.NewMemoryUserStore()
	sessionStore := auth.NewMemorySessionStore()

	oldHash, err := auth.HashPassword("OldPassword123!")
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	lastFailed := time.Now().UTC().Add(-time.Hour)
	lockedAt := time.Now().UTC().Add(-30 * time.Minute)
	lockoutUntil := time.Now().UTC().Add(30 * time.Minute)
	user := &auth.User{
		ID:                  "user-1",
		Username:            "admin",
		Role:                auth.RoleAdmin,
		PasswordHash:        oldHash,
		IsActive:            false,
		FailedLoginAttempts: 4,
		LastFailedLoginAt:   &lastFailed,
		LockedAt:            &lockedAt,
		LockoutUntil:        &lockoutUntil,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	session, err := auth.NewSession(user.ID, user.Role, time.Hour, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sessionStore.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := resetUserPassword(ctx, userStore, sessionStore, "admin", "NewPassword123!"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	got, err := userStore.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got == nil {
		t.Fatal("user not found after reset")
	}
	if err := auth.VerifyPassword("NewPassword123!", got.PasswordHash); err != nil {
		t.Fatalf("new password did not verify: %v", err)
	}
	if !got.IsActive {
		t.Error("reset should reactivate user")
	}
	if got.FailedLoginAttempts != 0 || got.LastFailedLoginAt != nil || got.LockedAt != nil || got.LockoutUntil != nil {
		t.Fatalf("lockout state not cleared: %+v", got)
	}
	sessions, err := sessionStore.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected sessions to be revoked, got %d", len(sessions))
	}
}

func TestResetUserPasswordNotFound(t *testing.T) {
	err := resetUserPassword(context.Background(), auth.NewMemoryUserStore(), auth.NewMemorySessionStore(), "missing", "NewPassword123!")
	if err == nil {
		t.Fatal("expected missing user error")
	}
}
