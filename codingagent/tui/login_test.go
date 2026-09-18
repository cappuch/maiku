package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLoginShowsChallengeAndCompletesOutsideConversation(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	m.loading = false
	finished := false
	cmd := m.startLogin("Miru", func(context.Context) (loginChallenge, error) {
		return loginChallenge{url: "https://example.com/activate", code: "ABCD", finish: func(context.Context) configResult {
			finished = true
			return configResult{notice: "Signed in"}
		}}, nil
	})
	_, poll := m.Update(cmd())
	if !strings.Contains(m.notice, "https://example.com/activate") || !strings.Contains(m.notice, "ABCD") || finished {
		t.Fatalf("challenge not displayed before polling: %q", m.notice)
	}
	m.Update(poll())
	if !finished || m.configBusy || m.login != nil || m.notice != "Signed in" || len(m.messages) != 0 {
		t.Fatalf("unexpected completed login state: %+v", m)
	}
}

func TestLoginCancellationIgnoresLateChallengeAndCompletion(t *testing.T) {
	for _, phase := range []string{"starting", "polling"} {
		t.Run(phase, func(t *testing.T) {
			m := newModel(context.Background(), t.TempDir(), options{})
			cmd := m.startLogin("Codex", func(context.Context) (loginChallenge, error) {
				return loginChallenge{url: "https://example.com", finish: func(ctx context.Context) configResult {
					if !errors.Is(ctx.Err(), context.Canceled) {
						t.Fatal("poll context was not cancelled")
					}
					return configResult{notice: "stale result"}
				}}, nil
			})
			attempt := m.login
			message := cmd()
			if phase == "polling" {
				_, cmd = m.Update(message)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if phase == "polling" {
				message = cmd()
			}
			_, next := m.Update(message)
			if next != nil || m.login != nil || m.configBusy || m.notice != "Login cancelled" || attempt.ctx.Err() == nil {
				t.Fatalf("cancelled login was revived: %q", m.notice)
			}
		})
	}
}

func TestLoginStartFailureUnblocksInput(t *testing.T) {
	m := newModel(context.Background(), t.TempDir(), options{})
	cmd := m.startLogin("Miru", func(context.Context) (loginChallenge, error) {
		return loginChallenge{}, errors.New("service unavailable")
	})
	m.Update(cmd())
	if m.configBusy || m.login != nil || !strings.Contains(m.notice, "service unavailable") {
		t.Fatalf("unexpected failure state: %q", m.notice)
	}
}

func TestCodexProviderUsesSubscriptionLogin(t *testing.T) {
	for _, id := range []string{"codex", "openai-codex"} {
		m := newModel(context.Background(), t.TempDir(), options{})
		cmd := m.startProviderForm(id)
		if cmd == nil || m.form != nil || m.login == nil || m.login.name != "Codex" {
			t.Fatalf("%s did not start subscription login", id)
		}
		m.cancelLogin()
	}
	m := newModel(context.Background(), t.TempDir(), options{})
	m.startProviderForm("")
	m.form.input.SetValue("openai-codex")
	m.updateForm(tea.KeyMsg{Type: tea.KeyEnter})
	if m.form != nil || m.login == nil || m.login.name != "Codex" {
		t.Fatal("provider wizard asked for an API key instead of subscription login")
	}
	m.cancelLogin()
}
