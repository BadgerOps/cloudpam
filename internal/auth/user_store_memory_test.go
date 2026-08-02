package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func usTestUser(id, username string) *User {
	return &User{
		ID:           id,
		Username:     username,
		Email:        username + "@example.test",
		Role:         RoleViewer,
		PasswordHash: []byte("hash-" + id),
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func TestMemoryUserStoreCreateRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		user *User
	}{
		{"nil user", nil},
		{"empty id", &User{Username: "alice"}},
		{"empty username", &User{ID: "u1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryUserStore()
			if err := store.Create(ctx, tt.user); !errors.Is(err, ErrInvalidSession) {
				t.Errorf("Create err = %v, want ErrInvalidSession", err)
			}
			users, err := store.List(ctx)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(users) != 0 {
				t.Errorf("invalid user was persisted: %v", users)
			}
		})
	}
}

func TestMemoryUserStoreCreateRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	if err := store.Create(ctx, usTestUser("u1", "alice")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Create(ctx, usTestUser("u1", "bob")); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate ID err = %v, want ErrUserExists", err)
	}
	if err := store.Create(ctx, usTestUser("u2", "alice")); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate username err = %v, want ErrUserExists", err)
	}

	users, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("store holds %d users, want 1", len(users))
	}
}

func TestMemoryUserStoreLookups(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()
	if err := store.Create(ctx, usTestUser("u1", "Alice")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("by id", func(t *testing.T) {
		got, err := store.GetByID(ctx, "u1")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got == nil || got.Username != "Alice" {
			t.Fatalf("GetByID = %+v, want Alice", got)
		}
	})

	t.Run("by username is case sensitive", func(t *testing.T) {
		got, err := store.GetByUsername(ctx, "Alice")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if got == nil || got.ID != "u1" {
			t.Fatalf("GetByUsername(Alice) = %+v, want u1", got)
		}

		lower, err := store.GetByUsername(ctx, "alice")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if lower != nil {
			t.Errorf("GetByUsername(alice) = %+v, want nil (case sensitive)", lower)
		}
	})

	t.Run("misses return nil without error", func(t *testing.T) {
		cases := []struct {
			name string
			fn   func() (*User, error)
		}{
			{"empty id", func() (*User, error) { return store.GetByID(ctx, "") }},
			{"unknown id", func() (*User, error) { return store.GetByID(ctx, "nope") }},
			{"empty username", func() (*User, error) { return store.GetByUsername(ctx, "") }},
			{"unknown username", func() (*User, error) { return store.GetByUsername(ctx, "nope") }},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got, err := c.fn()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != nil {
					t.Errorf("got %+v, want nil", got)
				}
			})
		}
	})
}

func TestMemoryUserStoreListStripsPasswordHashes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()
	for _, u := range []*User{usTestUser("u1", "alice"), usTestUser("u2", "bob")} {
		if err := store.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	users, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	for _, u := range users {
		if u.PasswordHash != nil {
			t.Errorf("List leaked the password hash for %q", u.Username)
		}
	}

	// The stripped list must not have removed hashes from the store itself.
	stored, err := store.GetByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(stored.PasswordHash) == 0 {
		t.Error("List() cleared the stored password hash")
	}
}

func TestMemoryUserStoreIsolatesStoredState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	input := usTestUser("u1", "alice")
	if err := store.Create(ctx, input); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Mutating the caller's struct after the write must not reach the store.
	input.Role = RoleAdmin
	input.Email = "attacker@example.test"
	input.PasswordHash[0] = 'X'

	got, err := store.GetByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Role != RoleViewer {
		t.Errorf("role escalated to %q via caller mutation", got.Role)
	}
	if got.Email == "attacker@example.test" {
		t.Error("email changed via caller mutation")
	}
	if got.PasswordHash[0] == 'X' {
		t.Error("password hash changed via caller mutation")
	}

	// Mutating the returned copy must not reach the store either.
	got.Role = RoleAdmin
	got.PasswordHash[0] = 'Y'
	again, err := store.GetByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if again.Role != RoleViewer {
		t.Errorf("role escalated to %q via returned-copy mutation", again.Role)
	}
	if again.PasswordHash[0] == 'Y' {
		t.Error("password hash changed via returned-copy mutation")
	}
}

func TestMemoryUserStoreCopiesOptionalTimestamps(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	last := time.Now().UTC().Add(-time.Hour)
	failed := last.Add(-time.Minute)
	locked := last.Add(-2 * time.Minute)
	until := last.Add(time.Minute)

	u := usTestUser("u1", "alice")
	u.LastLoginAt = &last
	u.LastFailedLoginAt = &failed
	u.LockedAt = &locked
	u.LockoutUntil = &until
	u.FailedLoginAttempts = 3
	if err := store.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	pairs := []struct {
		name       string
		got, input *time.Time
	}{
		{"LastLoginAt", got.LastLoginAt, u.LastLoginAt},
		{"LastFailedLoginAt", got.LastFailedLoginAt, u.LastFailedLoginAt},
		{"LockedAt", got.LockedAt, u.LockedAt},
		{"LockoutUntil", got.LockoutUntil, u.LockoutUntil},
	}
	for _, p := range pairs {
		if p.got == nil {
			t.Errorf("%s was dropped", p.name)
			continue
		}
		if p.got == p.input {
			t.Errorf("%s shares the caller's pointer", p.name)
		}
		if !p.got.Equal(*p.input) {
			t.Errorf("%s = %v, want %v", p.name, *p.got, *p.input)
		}
	}
	if got.FailedLoginAttempts != 3 {
		t.Errorf("FailedLoginAttempts = %d, want 3", got.FailedLoginAttempts)
	}
}

func TestMemoryUserStoreUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("nil and empty id rejected", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Update(ctx, nil); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("Update(nil) = %v, want ErrUserNotFound", err)
		}
		if err := store.Update(ctx, &User{Username: "x"}); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("Update(no id) = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("unknown id rejected", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Update(ctx, usTestUser("ghost", "ghost")); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("Update err = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("field update persists", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Create(ctx, usTestUser("u1", "alice")); err != nil {
			t.Fatalf("Create: %v", err)
		}
		updated := usTestUser("u1", "alice")
		updated.Role = RoleOperator
		updated.DisplayName = "Alice A"
		updated.IsActive = false
		if err := store.Update(ctx, updated); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := store.GetByID(ctx, "u1")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Role != RoleOperator || got.DisplayName != "Alice A" || got.IsActive {
			t.Errorf("update did not persist: %+v", got)
		}
	})

	t.Run("rename moves the username index", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Create(ctx, usTestUser("u1", "alice")); err != nil {
			t.Fatalf("Create: %v", err)
		}
		renamed := usTestUser("u1", "alicia")
		if err := store.Update(ctx, renamed); err != nil {
			t.Fatalf("Update: %v", err)
		}

		old, err := store.GetByUsername(ctx, "alice")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if old != nil {
			t.Errorf("old username still resolves: %+v", old)
		}
		neu, err := store.GetByUsername(ctx, "alicia")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if neu == nil || neu.ID != "u1" {
			t.Errorf("new username = %+v, want u1", neu)
		}
	})

	t.Run("rename to a taken username is rejected", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Create(ctx, usTestUser("u1", "alice")); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Create(ctx, usTestUser("u2", "bob")); err != nil {
			t.Fatalf("Create: %v", err)
		}

		clash := usTestUser("u1", "bob")
		if err := store.Update(ctx, clash); !errors.Is(err, ErrUserExists) {
			t.Fatalf("Update err = %v, want ErrUserExists", err)
		}

		// Both users must be untouched after the rejected rename.
		alice, err := store.GetByUsername(ctx, "alice")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if alice == nil || alice.ID != "u1" {
			t.Errorf("alice = %+v, want u1 intact", alice)
		}
		bob, err := store.GetByUsername(ctx, "bob")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if bob == nil || bob.ID != "u2" {
			t.Errorf("bob = %+v, want u2 intact", bob)
		}
	})
}

func TestMemoryUserStoreDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("empty and unknown ids rejected", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Delete(ctx, ""); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("Delete(\"\") = %v, want ErrUserNotFound", err)
		}
		if err := store.Delete(ctx, "ghost"); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("Delete(ghost) = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("delete frees the username", func(t *testing.T) {
		store := NewMemoryUserStore()
		if err := store.Create(ctx, usTestUser("u1", "alice")); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, "u1"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		got, err := store.GetByID(ctx, "u1")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got != nil {
			t.Errorf("user still present: %+v", got)
		}
		byName, err := store.GetByUsername(ctx, "alice")
		if err != nil {
			t.Fatalf("GetByUsername: %v", err)
		}
		if byName != nil {
			t.Errorf("username index not cleaned up: %+v", byName)
		}

		// The freed username can be reused.
		if err := store.Create(ctx, usTestUser("u2", "alice")); err != nil {
			t.Errorf("recreating with the freed username failed: %v", err)
		}
	})
}

func TestMemoryUserStoreGetByOIDCIdentity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	oidcUser := usTestUser("u1", "alice")
	oidcUser.AuthProvider = "oidc"
	oidcUser.OIDCIssuer = "https://idp.example.test"
	oidcUser.OIDCSubject = "sub-123"
	if err := store.Create(ctx, oidcUser); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(ctx, usTestUser("u2", "bob")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []struct {
		name    string
		issuer  string
		subject string
		wantID  string
	}{
		{"match", "https://idp.example.test", "sub-123", "u1"},
		{"empty issuer", "", "sub-123", ""},
		{"empty subject", "https://idp.example.test", "", ""},
		{"wrong issuer", "https://other.example.test", "sub-123", ""},
		{"wrong subject", "https://idp.example.test", "sub-999", ""},
		{"local user is not matched by empty identity", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetByOIDCIdentity(ctx, tt.issuer, tt.subject)
			if err != nil {
				t.Fatalf("GetByOIDCIdentity: %v", err)
			}
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("got %+v, want nil", got)
				}
				return
			}
			if got == nil || got.ID != tt.wantID {
				t.Errorf("got %+v, want ID %q", got, tt.wantID)
			}
		})
	}
}

func TestMemoryUserStoreUpdateLastLoginClearsLockout(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	past := time.Now().UTC().Add(-time.Hour)
	u := usTestUser("u1", "alice")
	u.FailedLoginAttempts = 5
	u.LastFailedLoginAt = &past
	u.LockedAt = &past
	lockout := past.Add(2 * time.Hour)
	u.LockoutUntil = &lockout
	if err := store.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.UpdateLastLogin(ctx, "ghost", time.Now()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UpdateLastLogin(ghost) = %v, want ErrUserNotFound", err)
	}

	now := time.Now().UTC()
	if err := store.UpdateLastLogin(ctx, "u1", now); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}

	got, err := store.GetByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.LastLoginAt == nil || !got.LastLoginAt.Equal(now) {
		t.Errorf("LastLoginAt = %v, want %v", got.LastLoginAt, now)
	}
	if got.FailedLoginAttempts != 0 {
		t.Errorf("FailedLoginAttempts = %d, want 0", got.FailedLoginAttempts)
	}
	if got.LastFailedLoginAt != nil {
		t.Errorf("LastFailedLoginAt = %v, want nil", got.LastFailedLoginAt)
	}
	if got.LockedAt != nil {
		t.Errorf("LockedAt = %v, want nil", got.LockedAt)
	}
	if got.LockoutUntil != nil {
		t.Errorf("LockoutUntil = %v, want nil", got.LockoutUntil)
	}
}

func TestMemoryUserStoreConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUserStore()

	const n = 20
	done := make(chan struct{}, n*2)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			_ = store.Create(ctx, usTestUser(id, "user-"+id))
		}(i)
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = store.List(ctx)
		}()
	}
	for i := 0; i < n*2; i++ {
		<-done
	}

	users, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != n {
		t.Errorf("got %d users, want %d", len(users), n)
	}
}
