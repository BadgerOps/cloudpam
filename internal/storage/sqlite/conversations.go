//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateConversation persists a new conversation.
func (s *Store) CreateConversation(ctx context.Context, conv domain.Conversation) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		conv.ID, conv.Title,
		conv.CreatedAt.Format(time.RFC3339), conv.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// GetConversation returns a conversation with its messages.
func (s *Store) GetConversation(ctx context.Context, id string) (*domain.ConversationWithMessages, error) {
	var conv domain.Conversation
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id,
	).Scan(&conv.ID, &conv.Title, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, created_at FROM conversation_messages WHERE conversation_id = ? ORDER BY created_at ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []domain.ConversationMessage
	for rows.Next() {
		var msg domain.ConversationMessage
		var msgCreatedAt string
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &msgCreatedAt); err != nil {
			return nil, err
		}
		msg.CreatedAt, _ = time.Parse(time.RFC3339, msgCreatedAt)
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
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Conversation
	for rows.Next() {
		var conv domain.Conversation
		var createdAt, updatedAt string
		if err := rows.Scan(&conv.ID, &conv.Title, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		result = append(result, conv)
	}
	if result == nil {
		result = []domain.Conversation{}
	}
	return result, rows.Err()
}

// DeleteConversation removes a conversation and its messages (via CASCADE).
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// AddMessage appends a message to a conversation and updates its updated_at.
func (s *Store) AddMessage(ctx context.Context, msg domain.ConversationMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Verify conversation exists
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM conversations WHERE id = ?`, msg.ConversationID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return storage.ErrNotFound
		}
		return err
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO conversation_messages (id, conversation_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		msg.ID, msg.ConversationID, msg.Role, msg.Content, msg.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now, msg.ConversationID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
