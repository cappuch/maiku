package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
	"github.com/mikus/maiku/codingagent/core"
	"strings"
	"testing"
)

func TestTranscriptStreamAndCompletion(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	partial := ai.Message{Role: "assistant", AssistantContent: []ai.AssistantContentBlock{{Type: "text", Text: "Hello"}}}
	m.Update(agent.AgentEvent{Type: agent.EventMessageUpdate, Message: partial})
	if !strings.Contains(m.View(), "Hello") {
		t.Fatal("streamed text missing")
	}
	m.Update(agent.AgentEvent{Type: agent.EventMessageEnd, Message: partial})
	m.Update(doneMsg{})
	if len(m.messages) != 1 || m.live != nil || m.busy {
		t.Fatal("completion did not settle transcript")
	}
	if strings.Count(m.viewport.View(), "Hello") != 1 {
		t.Fatal("duplicate final message")
	}
}

func TestPickerKeyboardAndResize(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.picker = "Models"
	m.choices = []choice{{label: "one"}, {label: "two"}}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatal("selection did not advance")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.picker != "" {
		t.Fatal("escape did not dismiss picker")
	}
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 12})
	if m.viewport.Width != 16 || m.viewport.Height < 1 {
		t.Fatal("invalid small terminal layout")
	}
}

func TestStructuredUserContent(t *testing.T) {
	text := renderMessage(ai.Message{Role: "user", UserContent: []map[string]string{{"type": "text", "text": "hello"}}})
	if !strings.Contains(text, "hello") || strings.Contains(text, "map[") {
		t.Fatalf("bad user rendering: %s", text)
	}
}

func TestComposerSubmissionDoesNotSendTwice(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.session = core.NewAgentSession(core.AgentSessionOptions{})
	defer m.session.Dispose()
	m.input.SetValue("first request")
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !m.busy || m.input.Value() != "" {
		t.Fatal("submission did not start a turn and clear the composer")
	}
	m.input.SetValue("next request")
	_, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || m.input.Value() != "next request" {
		t.Fatal("busy submission must retain the draft without starting another turn")
	}
}
