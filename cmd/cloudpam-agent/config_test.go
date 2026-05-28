package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestResolveAgentIDUsesExplicitID(t *testing.T) {
	want := uuid.New()
	cfg := &Config{AgentID: "  " + want.String() + "  "}

	got, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("ResolveAgentID: %v", err)
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveAgentIDUsesExistingFile(t *testing.T) {
	want := uuid.New()
	path := filepath.Join(t.TempDir(), "agent-id")
	if err := os.WriteFile(path, []byte(want.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write agent id file: %v", err)
	}

	got, err := (&Config{AgentIDFile: path}).ResolveAgentID()
	if err != nil {
		t.Fatalf("ResolveAgentID: %v", err)
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveAgentIDCreatesStableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "agent-id")
	cfg := &Config{AgentIDFile: path}

	first, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("first ResolveAgentID: %v", err)
	}
	if first == uuid.Nil {
		t.Fatal("expected generated agent id")
	}

	second, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("second ResolveAgentID: %v", err)
	}
	if second != first {
		t.Fatalf("expected stable id %s, got %s", first, second)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated id file: %v", err)
	}
	if string(data) != first.String()+"\n" {
		t.Fatalf("unexpected id file contents %q", string(data))
	}
}

func TestResolveAgentIDDeterministicFallback(t *testing.T) {
	cfg := &Config{
		ServerURL: "https://cloudpam.example.com",
		APIKey:    "cpk_test",
		AgentName: "aws-prod",
		AccountID: 125672604241,
	}

	first, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("first ResolveAgentID: %v", err)
	}
	second, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("second ResolveAgentID: %v", err)
	}
	if first != second {
		t.Fatalf("expected deterministic id %s, got %s", first, second)
	}

	cfg.APIKey = "cpk_rotated"
	rotatedKeyID, err := cfg.ResolveAgentID()
	if err != nil {
		t.Fatalf("rotated-key ResolveAgentID: %v", err)
	}
	if rotatedKeyID != first {
		t.Fatalf("fallback id should not depend on API key: expected %s, got %s", first, rotatedKeyID)
	}
}
