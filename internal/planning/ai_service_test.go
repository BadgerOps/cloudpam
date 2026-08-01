package planning

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/storage"
)

// covFakeProvider is a hermetic llm.Provider: it records the messages it was
// asked to complete and replays a canned stream.
type covFakeProvider struct {
	mu        sync.Mutex
	available bool
	events    []llm.StreamEvent
	streamErr error
	calls     int
	gotMsgs   []llm.Message
	gotOpts   llm.Options
}

func (p *covFakeProvider) Name() string { return "cov-fake" }

func (p *covFakeProvider) Available() bool { return p.available }

func (p *covFakeProvider) Complete(context.Context, []llm.Message, llm.Options) (*llm.Response, error) {
	return nil, errors.New("cov-fake: Complete not used")
}

func (p *covFakeProvider) StreamComplete(_ context.Context, messages []llm.Message, opts llm.Options) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.calls++
	p.gotMsgs = append([]llm.Message(nil), messages...)
	p.gotOpts = opts
	streamErr := p.streamErr
	events := p.events
	p.mu.Unlock()

	if streamErr != nil {
		return nil, streamErr
	}
	ch := make(chan llm.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (p *covFakeProvider) messages() []llm.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]llm.Message(nil), p.gotMsgs...)
}

func (p *covFakeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func covSetupAIService(t *testing.T, provider llm.Provider) (*AIPlanningService, *storage.MemoryStore, *storage.MemoryConversationStore) {
	t.Helper()
	st := storage.NewMemoryStore()
	convStore := storage.NewMemoryConversationStore(st)
	svc := NewAIPlanningService(NewAnalysisService(st), convStore, st, provider)
	return svc, st, convStore
}

func covDrain(t *testing.T, ch <-chan llm.StreamEvent) []llm.StreamEvent {
	t.Helper()
	var out []llm.StreamEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out
}

func TestCovNewAIPlanningServiceWiresDependencies(t *testing.T) {
	provider := &covFakeProvider{available: true}
	svc, st, convStore := covSetupAIService(t, provider)

	if svc.provider != provider {
		t.Error("provider not stored")
	}
	if svc.mainStore != st {
		t.Error("main store not stored")
	}
	if svc.convStore != convStore {
		t.Error("conversation store not stored")
	}
	if svc.analysis == nil {
		t.Error("analysis service not stored")
	}
}

func TestCovAIServiceAvailable(t *testing.T) {
	tests := []struct {
		name     string
		provider llm.Provider
		want     bool
	}{
		{"nil provider", nil, false},
		{"provider unavailable", &covFakeProvider{available: false}, false},
		{"provider available", &covFakeProvider{available: true}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := covSetupAIService(t, tc.provider)
			if got := svc.Available(); got != tc.want {
				t.Errorf("Available() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCovAIServiceConversationLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := covSetupAIService(t, &covFakeProvider{available: true})

	conv, err := svc.CreateConversation(ctx, "Plan prod")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if conv.ID == "" {
		t.Fatal("conversation ID is empty")
	}
	if conv.Title != "Plan prod" {
		t.Errorf("Title = %q, want Plan prod", conv.Title)
	}
	if conv.CreatedAt.IsZero() || conv.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: %+v", conv)
	}
	if !conv.CreatedAt.Equal(conv.UpdatedAt) {
		t.Errorf("CreatedAt (%v) and UpdatedAt (%v) should match on creation", conv.CreatedAt, conv.UpdatedAt)
	}

	fetched, err := svc.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if fetched.ID != conv.ID || fetched.Title != "Plan prod" {
		t.Errorf("fetched = %+v, want the created conversation", fetched.Conversation)
	}
	if len(fetched.Messages) != 0 {
		t.Errorf("messages = %d, want 0 for a new conversation", len(fetched.Messages))
	}

	second, err := svc.CreateConversation(ctx, "Plan dev")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if second.ID == conv.ID {
		t.Fatal("conversation IDs must be unique")
	}

	list, err := svc.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("conversations = %d, want 2", len(list))
	}

	if err := svc.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}
	if _, err := svc.GetConversation(ctx, conv.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetConversation() after delete error = %v, want ErrNotFound", err)
	}

	list, err = svc.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("remaining conversations = %+v, want only %s", list, second.ID)
	}
}

func TestCovAIServiceGetConversationUnknownID(t *testing.T) {
	svc, _, _ := covSetupAIService(t, &covFakeProvider{available: true})
	if _, err := svc.GetConversation(context.Background(), "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetConversation() error = %v, want ErrNotFound", err)
	}
}

func TestCovAIServiceDeleteConversationUnknownID(t *testing.T) {
	svc, _, _ := covSetupAIService(t, &covFakeProvider{available: true})
	if err := svc.DeleteConversation(context.Background(), "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("DeleteConversation() error = %v, want ErrNotFound", err)
	}
}

func TestCovAIServiceChatStreamsAndPersistsMessages(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{
		available: true,
		events: []llm.StreamEvent{
			{Delta: "Here is "},
			{Delta: "a plan."},
			{Done: true, FinishReason: "stop"},
		},
	}
	svc, _, _ := covSetupAIService(t, provider)

	conv, err := svc.CreateConversation(ctx, "session")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	ch, err := svc.Chat(ctx, conv.ID, "Plan a /16 for prod")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	events := covDrain(t, ch)

	if len(events) != 3 {
		t.Fatalf("streamed events = %d, want 3", len(events))
	}
	if events[0].Delta != "Here is " || events[1].Delta != "a plan." {
		t.Errorf("deltas = %q, %q", events[0].Delta, events[1].Delta)
	}
	if !events[2].Done || events[2].FinishReason != "stop" {
		t.Errorf("final event = %+v, want Done with finish reason stop", events[2])
	}

	stored, err := svc.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if len(stored.Messages) != 2 {
		t.Fatalf("persisted messages = %d, want user plus assistant", len(stored.Messages))
	}
	if stored.Messages[0].Role != "user" || stored.Messages[0].Content != "Plan a /16 for prod" {
		t.Errorf("user message = %+v", stored.Messages[0])
	}
	if stored.Messages[0].ConversationID != conv.ID {
		t.Errorf("user message conversation id = %q, want %q", stored.Messages[0].ConversationID, conv.ID)
	}
	if stored.Messages[1].Role != "assistant" {
		t.Errorf("assistant message role = %q", stored.Messages[1].Role)
	}
	if stored.Messages[1].Content != "Here is a plan." {
		t.Errorf("assistant content = %q, want the concatenated deltas", stored.Messages[1].Content)
	}
	if stored.Messages[1].ID == stored.Messages[0].ID {
		t.Error("message IDs must be unique")
	}
}

func TestCovAIServiceChatStopsAtDoneEvent(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{
		available: true,
		events: []llm.StreamEvent{
			{Delta: "kept"},
			{Done: true},
			{Delta: "-dropped"},
		},
	}
	svc, _, _ := covSetupAIService(t, provider)

	conv, _ := svc.CreateConversation(ctx, "session")
	ch, err := svc.Chat(ctx, conv.ID, "hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	events := covDrain(t, ch)
	if len(events) != 2 {
		t.Fatalf("streamed events = %d, want the stream to stop at Done", len(events))
	}

	stored, _ := svc.GetConversation(ctx, conv.ID)
	if stored.Messages[1].Content != "kept" {
		t.Fatalf("assistant content = %q, want content after Done to be dropped", stored.Messages[1].Content)
	}
}

func TestCovAIServiceChatSendsHistoryAndSkipsSystemMessages(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{available: true, events: []llm.StreamEvent{{Delta: "ok", Done: true}}}
	svc, _, convStore := covSetupAIService(t, provider)

	conv, _ := svc.CreateConversation(ctx, "session")
	if err := convStore.AddMessage(ctx, domain.ConversationMessage{
		ID: "m-sys", ConversationID: conv.ID, Role: "system", Content: "stale system note",
	}); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	if err := convStore.AddMessage(ctx, domain.ConversationMessage{
		ID: "m-prev", ConversationID: conv.ID, Role: "assistant", Content: "earlier reply",
	}); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}

	ch, err := svc.Chat(ctx, conv.ID, "follow up")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	covDrain(t, ch)

	msgs := provider.messages()
	if len(msgs) != 3 {
		t.Fatalf("provider messages = %d (%+v), want system prompt plus 2 history entries", len(msgs), msgs)
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first message role = %q, want the generated system prompt", msgs[0].Role)
	}
	if strings.Contains(msgs[0].Content, "stale system note") {
		t.Error("stored system messages must not be replayed to the provider")
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "earlier reply" {
		t.Errorf("history message = %+v", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "follow up" {
		t.Errorf("latest message = %+v", msgs[2])
	}
}

func TestCovAIServiceChatSystemPromptDescribesInstructions(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{available: true, events: []llm.StreamEvent{{Delta: "ok", Done: true}}}
	svc, _, _ := covSetupAIService(t, provider)

	conv, _ := svc.CreateConversation(ctx, "session")
	ch, err := svc.Chat(ctx, conv.ID, "hello")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	covDrain(t, ch)

	prompt := provider.messages()[0].Content
	for _, want := range []string{
		"CloudPAM's AI network planning assistant",
		"Valid pool types: supernet, region, environment, vpc, subnet.",
		"Pools must be in topological order (parent before child).",
		"Help the user plan their network infrastructure.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing %q:\n%s", want, prompt)
		}
	}
	// With no pools, the context sections must be omitted.
	if strings.Contains(prompt, "## Current Network Topology") {
		t.Error("empty store should not produce a topology section")
	}
	if strings.Contains(prompt, "## Available Address Space") {
		t.Error("empty store should not produce an address space section")
	}
}

func TestCovAIServiceChatSystemPromptIncludesNetworkContext(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{available: true, events: []llm.StreamEvent{{Delta: "ok", Done: true}}}
	svc, st, _ := covSetupAIService(t, provider)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	child, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Prod", CIDR: "10.0.1.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet, Description: "prod",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	conv, _ := svc.CreateConversation(ctx, "session")
	ch, err := svc.Chat(ctx, conv.ID, "what do I have?")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	covDrain(t, ch)

	prompt := provider.messages()[0].Content
	if !strings.Contains(prompt, "## Current Network Topology") {
		t.Fatalf("prompt missing topology section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Corp (10.0.0.0/16)") {
		t.Errorf("prompt missing the parent pool:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Prod (10.0.1.0/24)") {
		t.Errorf("prompt missing the child pool:\n%s", prompt)
	}
	if !strings.Contains(prompt, "(parent: ") {
		t.Errorf("prompt missing parent annotation for the child pool:\n%s", prompt)
	}
	if !strings.Contains(prompt, "## Available Address Space") {
		t.Errorf("prompt missing gap analysis section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "% utilized") {
		t.Errorf("prompt missing utilisation summary:\n%s", prompt)
	}
	_ = child
}

func TestCovAIServiceChatSystemPromptIncludesComplianceViolations(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{available: true, events: []llm.StreamEvent{{Delta: "ok", Done: true}}}
	svc, st, _ := covSetupAIService(t, provider)

	// A public CIDR triggers the RFC1918 compliance violation.
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Public", CIDR: "8.8.8.0/24", Type: domain.PoolTypeSubnet,
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	conv, _ := svc.CreateConversation(ctx, "session")
	ch, err := svc.Chat(ctx, conv.ID, "audit me")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	covDrain(t, ch)

	prompt := provider.messages()[0].Content
	if !strings.Contains(prompt, "## Current Compliance Issues") {
		t.Fatalf("prompt missing compliance section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "RFC1918-001") {
		t.Errorf("prompt missing the RFC1918 violation:\n%s", prompt)
	}
}

func TestCovAIServiceChatRejectsUnknownSession(t *testing.T) {
	provider := &covFakeProvider{available: true}
	svc, _, _ := covSetupAIService(t, provider)

	ch, err := svc.Chat(context.Background(), "no-such-session", "hi")
	if err == nil {
		t.Fatal("Chat() error = nil, want a persistence failure")
	}
	if ch != nil {
		t.Fatal("channel should be nil on error")
	}
	if !strings.Contains(err.Error(), "persist user message") {
		t.Errorf("error = %q, want persist user message", err.Error())
	}
	if provider.callCount() != 0 {
		t.Errorf("provider calls = %d, want 0 when the session is unknown", provider.callCount())
	}
}

func TestCovAIServiceChatPropagatesProviderError(t *testing.T) {
	ctx := context.Background()
	provider := &covFakeProvider{available: true, streamErr: errors.New("upstream unavailable")}
	svc, _, _ := covSetupAIService(t, provider)

	conv, _ := svc.CreateConversation(ctx, "session")
	ch, err := svc.Chat(ctx, conv.ID, "hi")
	if err == nil {
		t.Fatal("Chat() error = nil, want the provider failure")
	}
	if ch != nil {
		t.Fatal("channel should be nil on error")
	}
	if !strings.Contains(err.Error(), "stream complete") || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Errorf("error = %q, want the wrapped provider failure", err.Error())
	}

	// The user message is still persisted so the failed turn is visible.
	stored, err := svc.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if len(stored.Messages) != 1 || stored.Messages[0].Role != "user" {
		t.Fatalf("stored messages = %+v, want just the user message", stored.Messages)
	}
}

func TestCovExtractPlan(t *testing.T) {
	valid := "Sure, here you go:\n```json\n" + `{
  "name": "Prod Plan",
  "description": "prod layout",
  "pools": [
    {"ref": "root", "name": "Root", "cidr": "10.0.0.0/16", "type": "supernet"},
    {"ref": "child1", "name": "Child", "cidr": "10.0.1.0/24", "type": "subnet", "parent_ref": "root"}
  ]
}` + "\n```\nLet me know."

	plan, err := ExtractPlan(valid)
	if err != nil {
		t.Fatalf("ExtractPlan() error = %v", err)
	}
	if plan.Name != "Prod Plan" || plan.Description != "prod layout" {
		t.Errorf("plan = %+v", plan)
	}
	if len(plan.Pools) != 2 {
		t.Fatalf("pools = %d, want 2", len(plan.Pools))
	}
	if plan.Pools[0].Ref != "root" || plan.Pools[0].CIDR != "10.0.0.0/16" || plan.Pools[0].Type != "supernet" {
		t.Errorf("first pool = %+v", plan.Pools[0])
	}
	if plan.Pools[1].ParentRef != "root" {
		t.Errorf("second pool parent_ref = %q, want root", plan.Pools[1].ParentRef)
	}
}

func TestCovExtractPlanSkipsUnusableBlocks(t *testing.T) {
	content := "```json\n{not json}\n```\n" +
		"```json\n{\"name\":\"Empty\",\"pools\":[]}\n```\n" +
		"```json\n{\"name\":\"Good\",\"pools\":[{\"ref\":\"a\",\"cidr\":\"10.1.0.0/16\"}]}\n```"

	plan, err := ExtractPlan(content)
	if err != nil {
		t.Fatalf("ExtractPlan() error = %v", err)
	}
	if plan.Name != "Good" {
		t.Fatalf("plan.Name = %q, want the first block that parses with pools", plan.Name)
	}
	if len(plan.Pools) != 1 || plan.Pools[0].CIDR != "10.1.0.0/16" {
		t.Fatalf("pools = %+v", plan.Pools)
	}
}

func TestCovExtractPlanErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"no code block", "There is no plan here."},
		{"empty content", ""},
		{"unfenced json", `{"name":"x","pools":[{"ref":"a","cidr":"10.0.0.0/8"}]}`},
		{"non-json fence", "```yaml\nname: x\n```"},
		{"malformed json in block", "```json\n{\"name\": \n```"},
		{"block with no pools", "```json\n{\"name\":\"x\",\"pools\":[]}\n```"},
		{"block missing pools key", "```json\n{\"name\":\"x\"}\n```"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := ExtractPlan(tc.content)
			if err == nil {
				t.Fatalf("ExtractPlan() error = nil, want failure (got %+v)", plan)
			}
			if plan != nil {
				t.Fatalf("plan = %+v, want nil on error", plan)
			}
			if !strings.Contains(err.Error(), "no valid plan found") {
				t.Errorf("error = %q, want no valid plan found", err.Error())
			}
		})
	}
}
