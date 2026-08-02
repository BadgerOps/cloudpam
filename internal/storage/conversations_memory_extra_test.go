package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

var extraConvBase = time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)

func newConvStoreForExtraTests() *MemoryConversationStore {
	return NewMemoryConversationStore(NewMemoryStore())
}

func TestMemoryConversationStoreExtra_CreateGetDelete(t *testing.T) {
	ctx := context.Background()
	s := newConvStoreForExtraTests()

	conv := domain.Conversation{ID: "c1", Title: "Plan prod", CreatedAt: extraConvBase, UpdatedAt: extraConvBase}
	if err := s.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	got, err := s.GetConversation(ctx, "c1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "Plan prod" || !got.CreatedAt.Equal(extraConvBase) {
		t.Errorf("GetConversation = %+v, want the stored conversation", got.Conversation)
	}
	if got.Messages == nil {
		t.Error("Messages = nil, want an empty non-nil slice for a fresh conversation")
	}
	if len(got.Messages) != 0 {
		t.Errorf("len(Messages) = %d, want 0", len(got.Messages))
	}

	if _, err := s.GetConversation(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetConversation(missing) error = %v, want ErrNotFound", err)
	}

	if err := s.DeleteConversation(ctx, "c1"); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if _, err := s.GetConversation(ctx, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetConversation after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteConversation(ctx, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteConversation = %v, want ErrNotFound", err)
	}
}

func TestMemoryConversationStoreExtra_AddMessageAppendsAndTouchesConversation(t *testing.T) {
	ctx := context.Background()
	s := newConvStoreForExtraTests()

	if err := s.AddMessage(ctx, domain.ConversationMessage{ID: "m0", ConversationID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddMessage(unknown conversation) error = %v, want ErrNotFound", err)
	}

	if err := s.CreateConversation(ctx, domain.Conversation{ID: "c1", Title: "Chat", CreatedAt: extraConvBase, UpdatedAt: extraConvBase}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	msgs := []domain.ConversationMessage{
		{ID: "m1", ConversationID: "c1", Role: "user", Content: "hello", CreatedAt: extraConvBase},
		{ID: "m2", ConversationID: "c1", Role: "assistant", Content: "hi", CreatedAt: extraConvBase.Add(time.Second)},
		{ID: "m3", ConversationID: "c1", Role: "user", Content: "plan a /16", CreatedAt: extraConvBase.Add(2 * time.Second)},
	}
	for _, m := range msgs {
		if err := s.AddMessage(ctx, m); err != nil {
			t.Fatalf("AddMessage(%s): %v", m.ID, err)
		}
	}

	got, err := s.GetConversation(ctx, "c1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != len(msgs) {
		t.Fatalf("len(Messages) = %d, want %d", len(got.Messages), len(msgs))
	}
	for i, want := range msgs {
		if got.Messages[i].ID != want.ID || got.Messages[i].Content != want.Content {
			t.Errorf("Messages[%d] = %+v, want %+v (append order must be preserved)", i, got.Messages[i], want)
		}
	}
	if !got.UpdatedAt.After(extraConvBase) {
		t.Errorf("UpdatedAt = %v, want bumped past %v", got.UpdatedAt, extraConvBase)
	}
	if !got.CreatedAt.Equal(extraConvBase) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.CreatedAt, extraConvBase)
	}
}

func TestMemoryConversationStoreExtra_CreateWithExistingIDResetsMessages(t *testing.T) {
	ctx := context.Background()
	s := newConvStoreForExtraTests()

	if err := s.CreateConversation(ctx, domain.Conversation{ID: "c1", Title: "first"}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := s.AddMessage(ctx, domain.ConversationMessage{ID: "m1", ConversationID: "c1", Content: "hi"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if err := s.CreateConversation(ctx, domain.Conversation{ID: "c1", Title: "second"}); err != nil {
		t.Fatalf("CreateConversation(reuse id): %v", err)
	}
	got, err := s.GetConversation(ctx, "c1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "second" {
		t.Errorf("Title = %q, want second", got.Title)
	}
	if len(got.Messages) != 0 {
		t.Errorf("len(Messages) = %d, want 0 after re-create", len(got.Messages))
	}

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Errorf("len(conversations) = %d, want 1", len(convs))
	}
}

func TestMemoryConversationStoreExtra_ListSortedByUpdatedAtDesc(t *testing.T) {
	ctx := context.Background()
	s := newConvStoreForExtraTests()

	if convs, err := s.ListConversations(ctx); err != nil || len(convs) != 0 {
		t.Fatalf("ListConversations(empty) = (%d, %v), want (0, nil)", len(convs), err)
	}

	// Insert out of order to prove sorting is applied.
	for _, offset := range []int{1, 3, 0, 2} {
		id := string(rune('a' + offset))
		if err := s.CreateConversation(ctx, domain.Conversation{
			ID:        id,
			Title:     id,
			CreatedAt: extraConvBase,
			UpdatedAt: extraConvBase.Add(time.Duration(offset) * time.Hour),
		}); err != nil {
			t.Fatalf("CreateConversation(%s): %v", id, err)
		}
	}

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	want := []string{"d", "c", "b", "a"}
	if len(convs) != len(want) {
		t.Fatalf("len = %d, want %d", len(convs), len(want))
	}
	for i, id := range want {
		if convs[i].ID != id {
			t.Errorf("index %d = %q, want %q", i, convs[i].ID, id)
		}
	}

	// Adding a message moves that conversation to the front.
	if err := s.AddMessage(ctx, domain.ConversationMessage{ID: "m1", ConversationID: "a", Content: "bump"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	convs, err = s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if convs[0].ID != "a" {
		t.Errorf("first = %q, want a (AddMessage must refresh UpdatedAt)", convs[0].ID)
	}
}

func TestMemoryConversationStoreExtra_DeleteRemovesOnlyTargetConversation(t *testing.T) {
	ctx := context.Background()
	s := newConvStoreForExtraTests()

	for _, id := range []string{"c1", "c2"} {
		if err := s.CreateConversation(ctx, domain.Conversation{ID: id, Title: id, UpdatedAt: extraConvBase}); err != nil {
			t.Fatalf("CreateConversation(%s): %v", id, err)
		}
		if err := s.AddMessage(ctx, domain.ConversationMessage{ID: "m-" + id, ConversationID: id, Content: id}); err != nil {
			t.Fatalf("AddMessage(%s): %v", id, err)
		}
	}

	if err := s.DeleteConversation(ctx, "c1"); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 || convs[0].ID != "c2" {
		t.Fatalf("conversations = %+v, want only c2", convs)
	}

	survivor, err := s.GetConversation(ctx, "c2")
	if err != nil {
		t.Fatalf("GetConversation(c2): %v", err)
	}
	if len(survivor.Messages) != 1 || survivor.Messages[0].Content != "c2" {
		t.Errorf("c2 messages = %+v, want its own single message", survivor.Messages)
	}
}
