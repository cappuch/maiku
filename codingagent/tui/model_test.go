package tui

import (
	"context"
	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
	"github.com/cappuch/maiku/codingagent/core"
	tea "github.com/charmbracelet/bubbletea"
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

func TestComposerQueuesWhileBusy(t *testing.T) {
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
	if command != nil || m.input.Value() != "" || len(m.queue) != 1 || m.queue[0] != "next request" {
		t.Fatalf("busy submission did not queue: command=%v value=%q queue=%v", command != nil, m.input.Value(), m.queue)
	}
	m.input.SetValue("/mcp add")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queue) != 1 || m.form != nil || !strings.Contains(m.notice, "Finish or stop") {
		t.Fatal("commands must not be queued")
	}
	_, command = m.Update(doneMsg{gen: m.promptGen})
	if command == nil || !m.busy || len(m.queue) != 0 {
		t.Fatal("queued message was not sent when the turn finished")
	}
}

func TestStopClearsQueue(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.session = core.NewAgentSession(core.AgentSessionOptions{})
	defer m.session.Dispose()
	m.busy = true
	m.queue = []string{"later"}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.queue) != 0 || m.status != "Stopping…" {
		t.Fatalf("stop did not clear queue: %v %s", m.queue, m.status)
	}
}

func TestScrollWheelDoesNotTouchComposer(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("hello")
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.viewport.YOffset >= before {
		t.Fatal("wheel did not scroll the transcript")
	}
	if m.input.Value() != "hello" {
		t.Fatalf("wheel changed the composer: %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "hello" {
		t.Fatalf("up arrow changed a single-line composer: %q", m.input.Value())
	}
}

func TestComposerStripsTerminalScrollSequences(t *testing.T) {
	if got := stripTerminalScroll("hello\x1b[<64;12;40M world"); got != "hello world" {
		t.Fatalf("sgr: %q", got)
	}
	if got := stripTerminalScroll("hello<65;3;8M"); got != "hello" {
		t.Fatalf("bare: %q", got)
	}
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	m.input.SetValue("hello\x1b[<64;1;1M")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	if strings.Contains(m.input.Value(), "<64;") || !strings.Contains(m.input.Value(), "hello") {
		t.Fatalf("composer kept a scroll sequence: %q", m.input.Value())
	}
	view := m.composerView()
	if strings.Count(view, "\n") != composerLines-1 {
		t.Fatalf("composer is not a fixed window: %q", view)
	}
}
