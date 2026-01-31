// Package auth provides authentication and authorization for CloudPAM.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Session errors.
var (
	// ErrSessionNotFound indicates the session was not found.
	ErrSessionNotFound = errors.New("session not found")

	// ErrSessionExpired indicates the session has expired.
	ErrSessionExpired = errors.New("session expired")

	// ErrInvalidSession indicates the session is invalid.
	ErrInvalidSession = errors.New("invalid session")
)

// DefaultSessionDuration is the default session lifetime.
const DefaultSessionDuration = 24 * time.Hour

// SessionIDLength is the number of random bytes used for session IDs.
const SessionIDLength = 32

// Session represents a user session.
type Session struct {
	ID        string            `json:"id"`
	UserID    string            `json:"user_id"`
	Role      Role              `json:"role"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsValid returns true if the session is valid (not expired and has required fields).
func (s *Session) IsValid() bool {
	return s.ID != "" && s.UserID != "" && !s.IsExpired()
}

// TimeRemaining returns the duration until the session expires.
// Returns 0 if the session has already expired.
func (s *Session) TimeRemaining() time.Duration {
	remaining := time.Until(s.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SessionStore defines the interface for session persistence.
type SessionStore interface {
	// Create stores a new session.
	Create(ctx context.Context, session *Session) error

	// Get retrieves a session by its ID.
	// Returns nil, nil if not found.
	Get(ctx context.Context, id string) (*Session, error)

	// Delete removes a session by its ID.
	Delete(ctx context.Context, id string) error

	// DeleteByUserID removes all sessions for a specific user.
	DeleteByUserID(ctx context.Context, userID string) error

	// Cleanup removes all expired sessions.
	// Returns the number of sessions removed.
	Cleanup(ctx context.Context) (int, error)
}

// MemorySessionStore is an in-memory implementation of SessionStore.
// It is thread-safe and suitable for development and single-instance deployments.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by session ID

	// userIndex maps user ID to session IDs for fast lookup
	userIndex map[string]map[string]struct{}
}

// NewMemorySessionStore creates a new in-memory session store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions:  make(map[string]*Session),
		userIndex: make(map[string]map[string]struct{}),
	}
}

// Create stores a new session.
func (s *MemorySessionStore) Create(_ context.Context, session *Session) error {
	if session == nil {
		return ErrInvalidSession
	}
	if session.ID == "" {
		return ErrInvalidSession
	}
	if session.UserID == "" {
		return ErrInvalidSession
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate ID (unlikely with random generation)
	if _, exists := s.sessions[session.ID]; exists {
		return ErrInvalidSession
	}

	// Store a copy to prevent external mutation
	stored := copySession(session)
	s.sessions[session.ID] = stored

	// Update user index
	if s.userIndex[session.UserID] == nil {
		s.userIndex[session.UserID] = make(map[string]struct{})
	}
	s.userIndex[session.UserID][session.ID] = struct{}{}

	return nil
}

// Get retrieves a session by its ID.
// Returns nil, nil if not found.
// Returns an error if the session is expired.
func (s *MemorySessionStore) Get(_ context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, nil
	}

	s.mu.RLock()
	session, exists := s.sessions[id]
	s.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	if session.IsExpired() {
		return nil, ErrSessionExpired
	}

	return copySession(session), nil
}

// Delete removes a session by its ID.
func (s *MemorySessionStore) Delete(_ context.Context, id string) error {
	if id == "" {
		return ErrSessionNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[id]
	if !exists {
		return ErrSessionNotFound
	}

	// Remove from user index
	if s.userIndex[session.UserID] != nil {
		delete(s.userIndex[session.UserID], id)
		if len(s.userIndex[session.UserID]) == 0 {
			delete(s.userIndex, session.UserID)
		}
	}

	delete(s.sessions, id)
	return nil
}

// DeleteByUserID removes all sessions for a specific user.
func (s *MemorySessionStore) DeleteByUserID(_ context.Context, userID string) error {
	if userID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sessionIDs, exists := s.userIndex[userID]
	if !exists {
		return nil
	}

	for sessionID := range sessionIDs {
		delete(s.sessions, sessionID)
	}
	delete(s.userIndex, userID)

	return nil
}

// Cleanup removes all expired sessions.
// Returns the number of sessions removed.
func (s *MemorySessionStore) Cleanup(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now()

	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			// Remove from user index
			if s.userIndex[session.UserID] != nil {
				delete(s.userIndex[session.UserID], id)
				if len(s.userIndex[session.UserID]) == 0 {
					delete(s.userIndex, session.UserID)
				}
			}
			delete(s.sessions, id)
			count++
		}
	}

	return count, nil
}

// Count returns the total number of sessions in the store.
// This is primarily for testing and monitoring.
func (s *MemorySessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// CountByUser returns the number of sessions for a specific user.
// This is primarily for testing and monitoring.
func (s *MemorySessionStore) CountByUser(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.userIndex[userID])
}

// copySession creates a deep copy of a Session.
func copySession(session *Session) *Session {
	if session == nil {
		return nil
	}

	cpy := &Session{
		ID:        session.ID,
		UserID:    session.UserID,
		Role:      session.Role,
		CreatedAt: session.CreatedAt,
		ExpiresAt: session.ExpiresAt,
	}

	if session.Metadata != nil {
		cpy.Metadata = make(map[string]string, len(session.Metadata))
		for k, v := range session.Metadata {
			cpy.Metadata[k] = v
		}
	}

	return cpy
}

// GenerateSessionID generates a cryptographically secure session ID.
func GenerateSessionID() (string, error) {
	bytes := make([]byte, SessionIDLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// NewSession creates a new Session with the given parameters.
// It generates a new session ID and sets creation/expiration times.
func NewSession(userID string, role Role, duration time.Duration, metadata map[string]string) (*Session, error) {
	id, err := GenerateSessionID()
	if err != nil {
		return nil, err
	}

	if duration <= 0 {
		duration = DefaultSessionDuration
	}

	now := time.Now().UTC()
	session := &Session{
		ID:        id,
		UserID:    userID,
		Role:      role,
		CreatedAt: now,
		ExpiresAt: now.Add(duration),
	}

	if metadata != nil {
		session.Metadata = make(map[string]string, len(metadata))
		for k, v := range metadata {
			session.Metadata[k] = v
		}
	}

	return session, nil
}

// session context key
const sessionContextKey contextKey = "session"

// ContextWithSession returns a new context with the session stored in it.
func ContextWithSession(ctx context.Context, session *Session) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext retrieves the session from the context.
// Returns nil if no session is present.
func SessionFromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	session, ok := ctx.Value(sessionContextKey).(*Session)
	if !ok {
		return nil
	}
	return session
}
