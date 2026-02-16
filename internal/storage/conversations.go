package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// ConversationStore provides storage operations for AI planning conversations.
type ConversationStore interface {
	// CreateConversation persists a new conversation.
	CreateConversation(ctx context.Context, conv domain.Conversation) error

	// GetConversation returns a conversation with its messages.
	GetConversation(ctx context.Context, id string) (*domain.ConversationWithMessages, error)

	// ListConversations returns all conversations ordered by updated_at desc.
	ListConversations(ctx context.Context) ([]domain.Conversation, error)

	// DeleteConversation removes a conversation and its messages.
	DeleteConversation(ctx context.Context, id string) error

	// AddMessage appends a message to a conversation and updates its updated_at.
	AddMessage(ctx context.Context, msg domain.ConversationMessage) error
}
