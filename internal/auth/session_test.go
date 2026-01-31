package auth

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSession_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"future expiry", now.Add(time.Hour), false},
		{"past expiry", now.Add(-time.Hour), true},
		{"just expired", now.Add(-time.Second), true},
		{"not yet expired", now.Add(time.Minute), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{ExpiresAt: tt.expiresAt}
			got := s.IsExpired()
			if got != tt.want {
				t.Errorf("Session.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSession_IsValid(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{
			"valid session",
			&Session{ID: "abc", UserID: "user1", ExpiresAt: future},
			true,
		},
		{
			"expired session",
			&Session{ID: "abc", UserID: "user1", ExpiresAt: past},
			false,
		},
		{
			"missing ID",
			&Session{ID: "", UserID: "user1", ExpiresAt: future},
			false,
		},
		{
			"missing UserID",
			&Session{ID: "abc", UserID: "", ExpiresAt: future},
			false,
		},
		{
			"missing both",
			&Session{ID: "", UserID: "", ExpiresAt: future},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.session.IsValid()
			if got != tt.want {
				t.Errorf("Session.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSession_TimeRemaining(t *testing.T) {
	// Session expiring in 1 hour
	s := &Session{ExpiresAt: time.Now().Add(time.Hour)}
	remaining := s.TimeRemaining()
	if remaining < 59*time.Minute || remaining > 61*time.Minute {
		t.Errorf("TimeRemaining() = %v, expected around 1 hour", remaining)
	}

	// Expired session should return 0
	expired := &Session{ExpiresAt: time.Now().Add(-time.Hour)}
	if expired.TimeRemaining() != 0 {
		t.Errorf("Expired session should have 0 time remaining")
	}
}

func TestMemorySessionStore_Create(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	session := &Session{
		ID:        "session1",
		UserID:    "user1",
		Role:      RoleAdmin,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Create session
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify it was stored
	if store.Count() != 1 {
		t.Errorf("Count() = %d, want 1", store.Count())
	}

	// Try to create duplicate
	if err := store.Create(ctx, session); err != ErrInvalidSession {
		t.Errorf("Create() duplicate should return ErrInvalidSession, got %v", err)
	}

	// Create with nil should fail
	if err := store.Create(ctx, nil); err != ErrInvalidSession {
		t.Errorf("Create(nil) should return ErrInvalidSession, got %v", err)
	}

	// Create with empty ID should fail
	badSession := &Session{ID: "", UserID: "user1"}
	if err := store.Create(ctx, badSession); err != ErrInvalidSession {
		t.Errorf("Create(empty ID) should return ErrInvalidSession, got %v", err)
	}

	// Create with empty UserID should fail
	badSession2 := &Session{ID: "id", UserID: ""}
	if err := store.Create(ctx, badSession2); err != ErrInvalidSession {
		t.Errorf("Create(empty UserID) should return ErrInvalidSession, got %v", err)
	}
}

func TestMemorySessionStore_Get(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	// Create a valid session
	session := &Session{
		ID:        "session1",
		UserID:    "user1",
		Role:      RoleViewer,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Get existing session
	got, err := store.Get(ctx, "session1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil for existing session")
	}
	if got.ID != session.ID {
		t.Errorf("Get() ID = %q, want %q", got.ID, session.ID)
	}
	if got.UserID != session.UserID {
		t.Errorf("Get() UserID = %q, want %q", got.UserID, session.UserID)
	}
	if got.Role != session.Role {
		t.Errorf("Get() Role = %q, want %q", got.Role, session.Role)
	}

	// Get non-existent session
	got, err = store.Get(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Get() nonexistent error = %v", err)
	}
	if got != nil {
		t.Errorf("Get() nonexistent should return nil")
	}

	// Get with empty ID
	got, err = store.Get(ctx, "")
	if err != nil {
		t.Errorf("Get() empty ID error = %v", err)
	}
	if got != nil {
		t.Errorf("Get() empty ID should return nil")
	}

	// Get expired session should return error
	expiredSession := &Session{
		ID:        "expired",
		UserID:    "user1",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if err := store.Create(ctx, expiredSession); err != nil {
		t.Fatalf("Create expired session error = %v", err)
	}

	_, err = store.Get(ctx, "expired")
	if err != ErrSessionExpired {
		t.Errorf("Get() expired should return ErrSessionExpired, got %v", err)
	}
}

func TestMemorySessionStore_Delete(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	session := &Session{
		ID:        "session1",
		UserID:    "user1",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Delete existing session
	if err := store.Delete(ctx, "session1"); err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Verify it was deleted
	if store.Count() != 0 {
		t.Errorf("Count() after delete = %d, want 0", store.Count())
	}

	// Delete non-existent session
	if err := store.Delete(ctx, "nonexistent"); err != ErrSessionNotFound {
		t.Errorf("Delete() nonexistent should return ErrSessionNotFound, got %v", err)
	}

	// Delete with empty ID
	if err := store.Delete(ctx, ""); err != ErrSessionNotFound {
		t.Errorf("Delete() empty ID should return ErrSessionNotFound, got %v", err)
	}
}

func TestMemorySessionStore_DeleteByUserID(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	// Create multiple sessions for same user
	for i := 0; i < 3; i++ {
		session := &Session{
			ID:        "session" + string(rune('1'+i)),
			UserID:    "user1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if err := store.Create(ctx, session); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Create session for different user
	otherSession := &Session{
		ID:        "other",
		UserID:    "user2",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, otherSession); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Delete all sessions for user1
	if err := store.DeleteByUserID(ctx, "user1"); err != nil {
		t.Errorf("DeleteByUserID() error = %v", err)
	}

	// Verify user1 sessions deleted
	if store.CountByUser("user1") != 0 {
		t.Errorf("CountByUser(user1) = %d, want 0", store.CountByUser("user1"))
	}

	// Verify user2 session still exists
	if store.CountByUser("user2") != 1 {
		t.Errorf("CountByUser(user2) = %d, want 1", store.CountByUser("user2"))
	}

	// Delete for non-existent user (should not error)
	if err := store.DeleteByUserID(ctx, "nonexistent"); err != nil {
		t.Errorf("DeleteByUserID() nonexistent error = %v", err)
	}

	// Delete with empty userID (should not error)
	if err := store.DeleteByUserID(ctx, ""); err != nil {
		t.Errorf("DeleteByUserID() empty error = %v", err)
	}
}

func TestMemorySessionStore_Cleanup(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	// Create valid session
	valid := &Session{
		ID:        "valid",
		UserID:    "user1",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, valid); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Create expired sessions
	for i := 0; i < 3; i++ {
		expired := &Session{
			ID:        "expired" + string(rune('1'+i)),
			UserID:    "user1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		if err := store.Create(ctx, expired); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Run cleanup
	count, err := store.Cleanup(ctx)
	if err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}
	if count != 3 {
		t.Errorf("Cleanup() removed %d sessions, want 3", count)
	}

	// Verify only valid session remains
	if store.Count() != 1 {
		t.Errorf("Count() after cleanup = %d, want 1", store.Count())
	}

	// Verify valid session still accessible
	got, err := store.Get(ctx, "valid")
	if err != nil || got == nil {
		t.Errorf("Valid session should still be accessible after cleanup")
	}
}

func TestMemorySessionStore_Concurrency(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent creates
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			session := &Session{
				ID:        "session" + string(rune(i)),
				UserID:    "user" + string(rune(i%10)),
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(time.Hour),
			}
			_ = store.Create(ctx, session)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = store.Get(ctx, "session"+string(rune(i)))
		}(i)
	}
	wg.Wait()

	// Concurrent cleanup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, _ = store.Cleanup(ctx)
		}()
	}
	wg.Wait()
}

func TestGenerateSessionID(t *testing.T) {
	// Generate multiple IDs and check uniqueness
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := GenerateSessionID()
		if err != nil {
			t.Fatalf("GenerateSessionID() error = %v", err)
		}
		if id == "" {
			t.Error("GenerateSessionID() returned empty string")
		}
		if ids[id] {
			t.Errorf("GenerateSessionID() returned duplicate ID: %s", id)
		}
		ids[id] = true

		// Check length (32 bytes = 64 hex chars)
		if len(id) != SessionIDLength*2 {
			t.Errorf("GenerateSessionID() length = %d, want %d", len(id), SessionIDLength*2)
		}
	}
}

func TestNewSession(t *testing.T) {
	// Basic session creation
	session, err := NewSession("user1", RoleAdmin, time.Hour, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if session.ID == "" {
		t.Error("NewSession() ID should not be empty")
	}
	if session.UserID != "user1" {
		t.Errorf("NewSession() UserID = %q, want %q", session.UserID, "user1")
	}
	if session.Role != RoleAdmin {
		t.Errorf("NewSession() Role = %q, want %q", session.Role, RoleAdmin)
	}
	if !session.IsValid() {
		t.Error("NewSession() should create valid session")
	}

	// With metadata
	metadata := map[string]string{"ip": "192.168.1.1", "ua": "test-agent"}
	session, err = NewSession("user2", RoleViewer, time.Hour, metadata)
	if err != nil {
		t.Fatalf("NewSession() with metadata error = %v", err)
	}
	if session.Metadata["ip"] != "192.168.1.1" {
		t.Errorf("NewSession() metadata[ip] = %q, want %q", session.Metadata["ip"], "192.168.1.1")
	}

	// With zero duration (should use default)
	session, err = NewSession("user3", RoleOperator, 0, nil)
	if err != nil {
		t.Fatalf("NewSession() zero duration error = %v", err)
	}
	expectedExpiry := time.Now().Add(DefaultSessionDuration)
	if session.ExpiresAt.Before(expectedExpiry.Add(-time.Minute)) ||
		session.ExpiresAt.After(expectedExpiry.Add(time.Minute)) {
		t.Errorf("NewSession() with 0 duration should use default duration")
	}
}

func TestCopySession(t *testing.T) {
	original := &Session{
		ID:        "session1",
		UserID:    "user1",
		Role:      RoleAdmin,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Metadata:  map[string]string{"key": "value"},
	}

	copied := copySession(original)

	// Verify fields are copied
	if copied.ID != original.ID {
		t.Errorf("Copy ID = %q, want %q", copied.ID, original.ID)
	}
	if copied.UserID != original.UserID {
		t.Errorf("Copy UserID = %q, want %q", copied.UserID, original.UserID)
	}
	if copied.Role != original.Role {
		t.Errorf("Copy Role = %q, want %q", copied.Role, original.Role)
	}

	// Modify copy and verify original is unchanged
	copied.Metadata["key"] = "modified"
	if original.Metadata["key"] == "modified" {
		t.Error("copySession should create deep copy of metadata")
	}

	// Test nil input
	if copySession(nil) != nil {
		t.Error("copySession(nil) should return nil")
	}
}

func TestSessionContext(t *testing.T) {
	session := &Session{
		ID:        "session1",
		UserID:    "user1",
		Role:      RoleAdmin,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Store in context
	ctx := ContextWithSession(context.Background(), session)

	// Retrieve from context
	got := SessionFromContext(ctx)
	if got == nil {
		t.Fatal("SessionFromContext() returned nil")
	}
	if got.ID != session.ID {
		t.Errorf("SessionFromContext() ID = %q, want %q", got.ID, session.ID)
	}

	// Test with nil session
	ctx = ContextWithSession(context.Background(), nil)
	got = SessionFromContext(ctx)
	if got != nil {
		t.Error("SessionFromContext() should return nil for nil session")
	}

	// Test with nil context
	got = SessionFromContext(nil) //nolint:staticcheck // testing nil context handling
	if got != nil {
		t.Error("SessionFromContext(nil) should return nil")
	}

	// Test with context without session
	got = SessionFromContext(context.Background())
	if got != nil {
		t.Error("SessionFromContext() should return nil for context without session")
	}
}

func TestMemorySessionStore_GetReturnsCorrectSession(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	// Create sessions for different users
	sessions := []*Session{
		{ID: "s1", UserID: "user1", Role: RoleAdmin, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "s2", UserID: "user2", Role: RoleViewer, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "s3", UserID: "user1", Role: RoleOperator, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
	}

	for _, s := range sessions {
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Verify each session can be retrieved correctly
	for _, s := range sessions {
		got, err := store.Get(ctx, s.ID)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", s.ID, err)
		}
		if got.ID != s.ID {
			t.Errorf("Get(%q) ID = %q", s.ID, got.ID)
		}
		if got.UserID != s.UserID {
			t.Errorf("Get(%q) UserID = %q, want %q", s.ID, got.UserID, s.UserID)
		}
		if got.Role != s.Role {
			t.Errorf("Get(%q) Role = %q, want %q", s.ID, got.Role, s.Role)
		}
	}
}

// Benchmark session operations
func BenchmarkMemorySessionStore_Create(b *testing.B) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session := &Session{
			ID:        "session" + string(rune(i)),
			UserID:    "user1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		_ = store.Create(ctx, session)
	}
}

func BenchmarkMemorySessionStore_Get(b *testing.B) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	session := &Session{
		ID:        "session1",
		UserID:    "user1",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	_ = store.Create(ctx, session)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Get(ctx, "session1")
	}
}

func BenchmarkGenerateSessionID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GenerateSessionID()
	}
}
