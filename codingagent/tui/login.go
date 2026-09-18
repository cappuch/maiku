package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	codex "github.com/mikus/maiku/ai/auth/openaicodex"
	"github.com/mikus/maiku/codingagent/core"
	miru "github.com/takara-ai/miru-code"
)

type loginChallenge struct {
	url, code string
	finish    func(context.Context) configResult
}

type loginAttempt struct {
	ctx    context.Context
	cancel context.CancelFunc
	name   string
}

type loginStarted struct {
	attempt   *loginAttempt
	challenge loginChallenge
	err       error
}

type loginFinished struct {
	attempt *loginAttempt
	result  configResult
}

func (m *model) startLogin(name string, begin func(context.Context) (loginChallenge, error)) tea.Cmd {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	attempt := &loginAttempt{ctx: ctx, cancel: cancel, name: name}
	m.login = attempt
	m.configBusy = true
	m.status = "Starting " + name + " login…"
	m.showNotice(m.status + "\n\nEsc cancels login.")
	return func() tea.Msg {
		startCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		challenge, err := begin(startCtx)
		return loginStarted{attempt: attempt, challenge: challenge, err: err}
	}
}

func (m *model) handleLoginStarted(msg loginStarted) tea.Cmd {
	if m.login != msg.attempt {
		return nil
	}
	if msg.err != nil {
		msg.attempt.cancel()
		m.login = nil
		m.configBusy = false
		m.status = msg.attempt.name + " login failed"
		m.showNotice(m.status + ": " + msg.err.Error())
		return nil
	}
	m.status = "Waiting for " + msg.attempt.name + " login…"
	notice := fmt.Sprintf("%s login\n\nOpen this URL in your browser:\n%s", msg.attempt.name, msg.challenge.url)
	if msg.challenge.code != "" {
		notice += "\n\nCode: " + msg.challenge.code
	}
	m.showNotice(notice + "\n\nWaiting for approval. Esc cancels login.")
	return func() tea.Msg {
		return loginFinished{attempt: msg.attempt, result: msg.challenge.finish(msg.attempt.ctx)}
	}
}

func (m *model) cancelLogin() {
	if m.login == nil {
		return
	}
	m.login.cancel()
	m.login = nil
	m.configBusy = false
	m.status = "Login cancelled"
	m.showNotice(m.status)
}

func beginCodexLogin(ctx context.Context) (loginChallenge, error) {
	device, err := codex.StartDeviceAuth(ctx)
	if err != nil {
		return loginChallenge{}, err
	}
	return loginChallenge{url: device.VerificationURI, code: device.UserCode, finish: func(ctx context.Context) configResult {
		token, err := codex.PollDeviceAuth(ctx, device)
		if err != nil {
			return configResult{err: fmt.Errorf("Codex login: %w", err)}
		}
		if err := ctx.Err(); err != nil {
			return configResult{err: err}
		}
		err = core.DefaultAuthStorage().Write("openai-codex", core.Credential{
			Type: core.CredentialOAuth, Access: token.Access, Refresh: token.Refresh, Expires: token.Expires.UnixMilli(),
		})
		if err != nil {
			return configResult{err: err}
		}
		if err := core.RefreshProviderModels(ctx, "openai-codex"); err != nil {
			return configResult{notice: "Codex subscription connected. Use /models refresh to load its models.", rebuild: true}
		}
		return configResult{notice: "Codex subscription connected. Choose an openai-codex model.", rebuild: true, openModels: true}
	}}, nil
}

func beginMiruLogin(ctx context.Context) (loginChallenge, error) {
	session, err := miru.StartDeviceLogin(ctx)
	if err != nil {
		return loginChallenge{}, err
	}
	return loginChallenge{url: session.VerificationURL, code: session.UserCode, finish: func(ctx context.Context) configResult {
		if err := session.Finish(ctx); err != nil {
			return configResult{err: fmt.Errorf("Miru login: %w", err)}
		}
		return configResult{notice: "Miru login complete. Credentials saved for Maiku and Miru."}
	}}, nil
}
