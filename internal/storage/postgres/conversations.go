//go:build postgres

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateConversation persists a new conversation.
func (s *Store) CreateConversation(ctx context.Context, conv domain.Conversation) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ($1, $2, $3, $4)`,
		conv.ID, conv.Title, conv.CreatedAt, conv.UpdatedAt,
	)
	return err
}

// GetConversation returns a conversation with its messages.
func (s *Store) GetConversation(ctx context.Context, id string) (*domain.ConversationWithMessages, error) {
	var conv domain.Conversation
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id = $1`, id,
	).Scan(&conv.ID, &conv.Title, &conv.CreatedAt, &conv.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, role, content, created_at FROM conversation_messages WHERE conversation_id = $1 ORDER BY created_at ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []domain.ConversationMessage
	for rows.Next() {
		var msg domain.ConversationMessage
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	if messages == nil {
		messages = []domain.ConversationMessage{}
	}

	return &domain.ConversationWithMessages{
		Conversation: conv,
		Messages:     messages,
	}, rows.Err()
}

// ListConversations returns all conversations ordered by updated_at desc.
func (s *Store) ListConversations(ctx context.Context) ([]domain.Conversation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Conversation
	for rows.Next() {
		var conv domain.Conversation
		if err := rows.Scan(&conv.ID, &conv.Title, &conv.CreatedAt, &conv.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, conv)
	}
	if result == nil {
		result = []domain.Conversation{}
	}
	return result, rows.Err()
}

// DeleteConversation removes a conversation and its messages (via CASCADE).
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// AddMessage appends a message to a conversation and updates its updated_at.
func (s *Store) AddMessage(ctx context.Context, msg domain.ConversationMessage) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Verify conversation exists
	var exists int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM conversations WHERE id = $1`, msg.ConversationID).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return storage.ErrNotFound
		}
		return err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO conversation_messages (id, conversation_id, role, content, created_at) VALUES ($1, $2, $3, $4, $5)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, msg.CreatedAt,
	)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE conversations SET updated_at = $1 WHERE id = $2`, now, msg.ConversationID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
