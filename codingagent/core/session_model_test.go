package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cappuch/maiku/ai"
)

func TestSetModelSurvivesReloadWithoutCreatingEmptySession(t *testing.T) {
	dir := t.TempDir()
	session := NewSessionManager("/work", dir, true)
	if err := session.SetModel("anthropic", "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.File()); !os.IsNotExist(err) {
		t.Fatalf("selecting a model created %s: %v", session.File(), err)
	}

	if err := session.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSessionManager(session.File())
	if err != nil {
		t.Fatal(err)
	}
	provider, modelID := loaded.RememberedModel()
	if provider != "anthropic" || modelID != "claude-sonnet-4-5" {
		t.Fatalf("remembered %s/%s", provider, modelID)
	}

	if err := loaded.SetModel("openai", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	again, err := LoadSessionManager(session.File())
	if err != nil {
		t.Fatal(err)
	}
	provider, modelID = again.RememberedModel()
	if provider != "openai" || modelID != "gpt-4o" {
		t.Fatalf("updated model %s/%s", provider, modelID)
	}
	if len(again.Messages()) != 1 {
		t.Fatalf("messages = %d", len(again.Messages()))
	}
}

func TestRememberedModelFallsBackToAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.jsonl")
	msg := ai.Message{
		Role:             "assistant",
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-5",
		AssistantContent: []ai.AssistantContentBlock{{Type: "text", Text: "hi"}},
	}
	encoded, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	content := `{"type":"session","version":1,"id":"old","timestamp":"2024-01-01T00:00:00.000Z","cwd":"/work"}` + "\n" +
		`{"type":"message","id":"m1","timestamp":"2024-01-01T00:00:00.000Z","message":` + string(encoded) + "}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSessionManager(path)
	if err != nil {
		t.Fatal(err)
	}
	provider, modelID := loaded.RememberedModel()
	if provider != "anthropic" || modelID != "claude-sonnet-4-5" {
		t.Fatalf("remembered %s/%s", provider, modelID)
	}
}

func TestAssistantMessageUpdatesRememberedModel(t *testing.T) {
	dir := t.TempDir()
	session := NewSessionManager("/work", dir, true)
	if err := session.SetModel("anthropic", "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ai.Message{
		Role:             "assistant",
		Provider:         "openai",
		Model:            "gpt-4o",
		AssistantContent: []ai.AssistantContentBlock{{Type: "text", Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSessionManager(session.File())
	if err != nil {
		t.Fatal(err)
	}
	provider, modelID := loaded.RememberedModel()
	if provider != "openai" || modelID != "gpt-4o" {
		t.Fatalf("remembered %s/%s", provider, modelID)
	}
	data, err := os.ReadFile(session.File())
	if err != nil {
		t.Fatal(err)
	}
	header, _, _ := strings.Cut(string(data), "\n")
	if !strings.Contains(header, `"model":"gpt-4o"`) || !strings.Contains(header, `"provider":"openai"`) {
		t.Fatalf("header = %s", header)
	}
}

func TestDiscardIfEmptyRemovesHeaderOnlySession(t *testing.T) {
	dir := t.TempDir()
	empty := NewSessionManager("/work", dir, true)
	if err := empty.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := empty.DiscardIfEmpty(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty.File()); !os.IsNotExist(err) {
		t.Fatalf("empty session still present: %v", err)
	}

	kept := NewSessionManager("/work", dir, true)
	if err := kept.AppendMessage(ai.Message{
		Role:        "user",
		UserContent: []ai.TextContent{{Type: "text", Text: "hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := kept.DiscardIfEmpty(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept.File()); err != nil {
		t.Fatalf("chat session removed: %v", err)
	}
}
