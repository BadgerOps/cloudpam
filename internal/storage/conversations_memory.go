package storage

import (
	"context"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

// MemoryConversationStore is an in-memory implementation of ConversationStore.
type MemoryConversationStore struct {
	store    *MemoryStore // shared mutex
	convs    map[string]domain.Conversation
	messages map[string][]domain.ConversationMessage // keyed by conversation ID
}

// NewMemoryConversationStore creates a new in-memory conversation store.
func NewMemoryConversationStore(store *MemoryStore) *MemoryConversationStore {
	return &MemoryConversationStore{
		store:    store,
		convs:    make(map[string]domain.Conversation),
		messages: make(map[string][]domain.ConversationMessage),
	}
}

func (m *MemoryConversationStore) CreateConversation(_ context.Context, conv domain.Conversation) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	m.convs[conv.ID] = conv
	m.messages[conv.ID] = []domain.ConversationMessage{}
	return nil
}

func (m *MemoryConversationStore) GetConversation(_ context.Context, id string) (*domain.ConversationWithMessages, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	conv, ok := m.convs[id]
	if !ok {
		return nil, ErrNotFound
	}

	msgs := m.messages[id]
	if msgs == nil {
		msgs = []domain.ConversationMessage{}
	}

	return &domain.ConversationWithMessages{
		Conversation: conv,
		Messages:     msgs,
	}, nil
}

func (m *MemoryConversationStore) ListConversations(_ context.Context) ([]domain.Conversation, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()

	result := make([]domain.Conversation, 0, len(m.convs))
	for _, c := range m.convs {
		result = append(result, c)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})

	return result, nil
}

func (m *MemoryConversationStore) DeleteConversation(_ context.Context, id string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	if _, ok := m.convs[id]; !ok {
		return ErrNotFound
	}
	delete(m.convs, id)
	delete(m.messages, id)
	return nil
}

func (m *MemoryConversationStore) AddMessage(_ context.Context, msg domain.ConversationMessage) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	conv, ok := m.convs[msg.ConversationID]
	if !ok {
		return ErrNotFound
	}

	m.messages[msg.ConversationID] = append(m.messages[msg.ConversationID], msg)
	conv.UpdatedAt = time.Now().UTC()
	m.convs[msg.ConversationID] = conv
	return nil
}
