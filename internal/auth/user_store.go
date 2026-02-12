package auth

import (
	"context"
	"sync"
	"time"
)

// UserStore defines the interface for user persistence.
type UserStore interface {
	// Create stores a new user.
	Create(ctx context.Context, user *User) error

	// GetByID retrieves a user by ID.
	// Returns nil, nil if not found.
	GetByID(ctx context.Context, id string) (*User, error)

	// GetByUsername retrieves a user by username (case-sensitive).
	// Returns nil, nil if not found.
	GetByUsername(ctx context.Context, username string) (*User, error)

	// List returns all users.
	List(ctx context.Context) ([]*User, error)

	// Update modifies an existing user.
	Update(ctx context.Context, user *User) error

	// Delete removes a user by ID (hard delete).
	Delete(ctx context.Context, id string) error

	// UpdateLastLogin sets the last_login_at timestamp for a user.
	UpdateLastLogin(ctx context.Context, id string, t time.Time) error
}

// MemoryUserStore is an in-memory implementation of UserStore.
// Thread-safe; suitable for development and single-instance deployments.
type MemoryUserStore struct {
	mu            sync.RWMutex
	users         map[string]*User // keyed by ID
	usernameIndex map[string]string // username -> ID
}

// NewMemoryUserStore creates a new in-memory user store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users:         make(map[string]*User),
		usernameIndex: make(map[string]string),
	}
}

func (s *MemoryUserStore) Create(_ context.Context, user *User) error {
	if user == nil || user.ID == "" || user.Username == "" {
		return ErrInvalidSession // reuse general invalid error
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[user.ID]; exists {
		return ErrUserExists
	}
	if _, exists := s.usernameIndex[user.Username]; exists {
		return ErrUserExists
	}

	stored := copyUser(user)
	s.users[user.ID] = stored
	s.usernameIndex[user.Username] = user.ID
	return nil
}

func (s *MemoryUserStore) GetByID(_ context.Context, id string) (*User, error) {
	if id == "" {
		return nil, nil
	}

	s.mu.RLock()
	user, exists := s.users[id]
	s.mu.RUnlock()

	if !exists {
		return nil, nil
	}
	return copyUser(user), nil
}

func (s *MemoryUserStore) GetByUsername(_ context.Context, username string) (*User, error) {
	if username == "" {
		return nil, nil
	}

	s.mu.RLock()
	id, exists := s.usernameIndex[username]
	if !exists {
		s.mu.RUnlock()
		return nil, nil
	}
	user := s.users[id]
	s.mu.RUnlock()

	return copyUser(user), nil
}

func (s *MemoryUserStore) List(_ context.Context) ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		cpy := copyUser(u)
		cpy.PasswordHash = nil // never expose hashes in list
		result = append(result, cpy)
	}
	return result, nil
}

func (s *MemoryUserStore) Update(_ context.Context, user *User) error {
	if user == nil || user.ID == "" {
		return ErrUserNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.users[user.ID]
	if !exists {
		return ErrUserNotFound
	}

	// If username changed, update index
	if existing.Username != user.Username {
		if _, taken := s.usernameIndex[user.Username]; taken {
			return ErrUserExists
		}
		delete(s.usernameIndex, existing.Username)
		s.usernameIndex[user.Username] = user.ID
	}

	s.users[user.ID] = copyUser(user)
	return nil
}

func (s *MemoryUserStore) Delete(_ context.Context, id string) error {
	if id == "" {
		return ErrUserNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return ErrUserNotFound
	}

	delete(s.usernameIndex, user.Username)
	delete(s.users, id)
	return nil
}

func (s *MemoryUserStore) UpdateLastLogin(_ context.Context, id string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[id]
	if !exists {
		return ErrUserNotFound
	}

	user.LastLoginAt = &t
	return nil
}
