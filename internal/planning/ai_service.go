package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/storage"
)

// AIPlanningService orchestrates LLM-powered network planning conversations.
type AIPlanningService struct {
	analysis  *AnalysisService
	convStore storage.ConversationStore
	mainStore storage.Store
	provider  llm.Provider
}

// NewAIPlanningService creates a new AI planning service.
func NewAIPlanningService(
	analysis *AnalysisService,
	convStore storage.ConversationStore,
	mainStore storage.Store,
	provider llm.Provider,
) *AIPlanningService {
	return &AIPlanningService{
		analysis:  analysis,
		convStore: convStore,
		mainStore: mainStore,
		provider:  provider,
	}
}

// Available returns true if the LLM provider is configured and ready.
func (s *AIPlanningService) Available() bool {
	return s.provider != nil && s.provider.Available()
}

// CreateConversation creates a new conversation.
func (s *AIPlanningService) CreateConversation(ctx context.Context, title string) (*domain.Conversation, error) {
	now := time.Now().UTC()
	conv := domain.Conversation{
		ID:        uuid.New().String(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.convStore.CreateConversation(ctx, conv); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return &conv, nil
}

// GetConversation returns a conversation with its messages.
func (s *AIPlanningService) GetConversation(ctx context.Context, id string) (*domain.ConversationWithMessages, error) {
	return s.convStore.GetConversation(ctx, id)
}

// ListConversations returns all conversations.
func (s *AIPlanningService) ListConversations(ctx context.Context) ([]domain.Conversation, error) {
	return s.convStore.ListConversations(ctx)
}

// DeleteConversation deletes a conversation.
func (s *AIPlanningService) DeleteConversation(ctx context.Context, id string) error {
	return s.convStore.DeleteConversation(ctx, id)
}

// Chat sends a user message and streams the assistant response. It persists
// both the user message and the final assistant message. The returned channel
// receives incremental text deltas; the caller should read until close.
func (s *AIPlanningService) Chat(ctx context.Context, sessionID, userMessage string) (<-chan llm.StreamEvent, error) {
	// Persist user message
	now := time.Now().UTC()
	userMsg := domain.ConversationMessage{
		ID:             uuid.New().String(),
		ConversationID: sessionID,
		Role:           "user",
		Content:        userMessage,
		CreatedAt:      now,
	}
	if err := s.convStore.AddMessage(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	// Load conversation history
	conv, err := s.convStore.GetConversation(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load conversation: %w", err)
	}

	// Build message list with system prompt
	systemPrompt, err := s.buildSystemPrompt(ctx)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
	}
	for _, m := range conv.Messages {
		if m.Role == "system" {
			continue
		}
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}

	// Start streaming from LLM
	eventCh, err := s.provider.StreamComplete(ctx, messages, llm.Options{})
	if err != nil {
		return nil, fmt.Errorf("stream complete: %w", err)
	}

	// Wrap the channel to capture the full response and persist it
	outCh := make(chan llm.StreamEvent, 64)
	go func() {
		defer close(outCh)
		var fullContent strings.Builder

		for evt := range eventCh {
			fullContent.WriteString(evt.Delta)
			outCh <- evt
			if evt.Done {
				break
			}
		}

		// Persist assistant message
		assistantMsg := domain.ConversationMessage{
			ID:             uuid.New().String(),
			ConversationID: sessionID,
			Role:           "assistant",
			Content:        fullContent.String(),
			CreatedAt:      time.Now().UTC(),
		}
		// Use background context since the request context may be cancelled
		_ = s.convStore.AddMessage(context.Background(), assistantMsg)
	}()

	return outCh, nil
}

// buildSystemPrompt generates a system prompt with current network context.
func (s *AIPlanningService) buildSystemPrompt(ctx context.Context) (string, error) {
	var sb strings.Builder

	sb.WriteString(`You are CloudPAM's AI network planning assistant. You help design IP address allocation schemes for cloud and on-premises networks.

You have access to the current network topology and can generate structured plans.

When the user asks you to create a plan, output it as a JSON code block with the following structure:
` + "```json" + `
{
  "name": "Plan Name",
  "description": "Description of the plan",
  "pools": [
    {"ref": "root", "name": "Pool Name", "cidr": "10.0.0.0/16", "type": "supernet"},
    {"ref": "child1", "name": "Child Pool", "cidr": "10.0.1.0/24", "type": "subnet", "parent_ref": "root"}
  ]
}
` + "```" + `

Valid pool types: supernet, region, environment, vpc, subnet.
Pools must be in topological order (parent before child).
CIDRs must be valid IPv4 with prefix length between /8 and /30.

`)

	// Add current pool hierarchy context
	pools, err := s.mainStore.ListPools(ctx)
	if err == nil && len(pools) > 0 {
		sb.WriteString("## Current Network Topology\n\n")
		for _, p := range pools {
			parentInfo := ""
			if p.ParentID != nil {
				parentInfo = fmt.Sprintf(" (parent: %d)", *p.ParentID)
			}
			sb.WriteString(fmt.Sprintf("- Pool %d: %s (%s) [%s]%s\n", p.ID, p.Name, p.CIDR, p.Type, parentInfo))
		}
		sb.WriteString("\n")
	}

	// Add gap analysis for top-level pools
	if len(pools) > 0 {
		var topLevelIDs []int64
		for _, p := range pools {
			if p.ParentID == nil {
				topLevelIDs = append(topLevelIDs, p.ID)
			}
		}
		if len(topLevelIDs) > 0 {
			sb.WriteString("## Available Address Space\n\n")
			for _, id := range topLevelIDs {
				gap, err := s.analysis.AnalyzeGaps(ctx, id)
				if err == nil {
					sb.WriteString(fmt.Sprintf("- Pool %d: %d total addresses, %d used, %d available (%.1f%% utilized)\n",
						id, gap.TotalAddresses, gap.UsedAddresses,
						gap.TotalAddresses-gap.UsedAddresses,
						float64(gap.UsedAddresses)/float64(gap.TotalAddresses)*100))
				}
			}
			sb.WriteString("\n")
		}

		// Add compliance context
		allIDs := make([]int64, len(pools))
		for i, p := range pools {
			allIDs[i] = p.ID
		}
		compliance, err := s.analysis.CheckCompliance(ctx, allIDs, false)
		if err == nil && compliance != nil && len(compliance.Violations) > 0 {
			sb.WriteString("## Current Compliance Issues\n\n")
			for _, v := range compliance.Violations {
				sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", v.Severity, v.RuleID, v.Message))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Help the user plan their network infrastructure. Be concise and practical.\n")

	return sb.String(), nil
}

// jsonBlockRe matches fenced JSON code blocks in markdown.
var jsonBlockRe = regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")

// ExtractPlan attempts to parse a GeneratedPlan from an assistant message.
func ExtractPlan(content string) (*domain.GeneratedPlan, error) {
	matches := jsonBlockRe.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		var plan domain.GeneratedPlan
		if err := json.Unmarshal([]byte(match[1]), &plan); err != nil {
			continue
		}
		if len(plan.Pools) > 0 {
			return &plan, nil
		}
	}
	return nil, fmt.Errorf("no valid plan found in message")
}
