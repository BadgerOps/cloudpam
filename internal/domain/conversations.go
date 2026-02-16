package domain

import (
	"time"
)

// Conversation represents an AI planning chat session.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationMessage represents a single message in a conversation.
type ConversationMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"` // "system", "user", "assistant"
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// ConversationWithMessages is a conversation with its full message history.
type ConversationWithMessages struct {
	Conversation
	Messages []ConversationMessage `json:"messages"`
}

// ChatRequest is the input for the AI chat endpoint.
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// GeneratedPlan is the structured plan output from the LLM.
type GeneratedPlan struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Pools       []PoolSpec `json:"pools"`
}

// PoolSpec describes a single pool in a generated plan.
type PoolSpec struct {
	Ref       string `json:"ref"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Type      string `json:"type"`
	ParentRef string `json:"parent_ref,omitempty"`
}

// ApplyPlanRequest is the input for applying a generated plan.
type ApplyPlanRequest struct {
	Plan          GeneratedPlan `json:"plan"`
	SkipConflicts bool          `json:"skip_conflicts"`
}
