package main

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/cappuch/maiku/codingagent/core"
	"github.com/cappuch/maiku/updater"
	"github.com/wailsapp/wails/v2/pkg/menu"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Desktop updates use native dialogs, including the standard Yes/No buttons on
// Windows and Linux. Installation never quits an active agent session.
type desktopUpdater struct {
	client    *updater.Client
	mu        sync.Mutex
	installed bool
	ctx       context.Context
	cancel    context.CancelFunc
	cancelMu  sync.Mutex
}

func newDesktopUpdater() *desktopUpdater {
	return &desktopUpdater{client: updater.New("desktop")}
}

func (u *desktopUpdater) menu() *menu.Menu {
	m := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		m.Append(menu.AppMenu())
		m.Append(menu.EditMenu())
	}
	help := m.AddSubmenu("Help")
	help.AddText("Check for Updates…", nil, func(_ *menu.CallbackData) { go u.check(true) })
	return m
}

func (u *desktopUpdater) start(ctx context.Context) {
	u.mu.Lock()
	if u.ctx != nil {
		u.mu.Unlock()
		return
	}
	u.cancelMu.Lock()
	u.ctx, u.cancel = context.WithCancel(ctx)
	u.cancelMu.Unlock()
	updateCtx := u.ctx
	u.mu.Unlock()
	if !u.client.Enabled() {
		return
	}
	go func() {
		u.check(false)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-updateCtx.Done():
				return
			case <-ticker.C:
				u.check(false)
			}
		}
	}()
}

func (u *desktopUpdater) stop(context.Context) {
	// Cancellation must not wait for a check/download holding the UI mutex.
	u.cancelMu.Lock()
	defer u.cancelMu.Unlock()
	if u.cancel != nil {
		u.cancel()
	}
}

func (u *desktopUpdater) message(kind wruntime.DialogType, message string) {
	if u.ctx.Err() != nil {
		return
	}
	_, _ = wruntime.MessageDialog(u.ctx, wruntime.MessageDialogOptions{
		Type: kind, Title: "maiku updates", Message: message, Buttons: []string{"Ok"},
	})
}

func (u *desktopUpdater) check(manual bool) {
	if !u.mu.TryLock() {
		return
	}
	defer u.mu.Unlock()
	if u.ctx == nil || u.ctx.Err() != nil {
		return
	}
	if !manual && !desktopAutomaticUpdatesEnabled() {
		return
	}
	if u.installed {
		if manual {
			u.message(wruntime.InfoDialog, "The update is installed. Quit and reopen maiku when you're ready to use it.")
		}
		return
	}
	ctx, cancel := context.WithTimeout(u.ctx, 20*time.Second)
	update, err := u.client.Check(ctx)
	cancel()
	if err != nil {
		if manual {
			u.message(wruntime.ErrorDialog, err.Error())
		}
		return
	}
	if update == nil {
		if manual {
			u.message(wruntime.InfoDialog, "You're running the latest available build.")
		}
		return
	}
	if u.ctx.Err() != nil {
		return
	}
	// Respect a preference changed while the network check was in flight.
	if !manual && !desktopAutomaticUpdatesEnabled() {
		return
	}
	answer, err := wruntime.MessageDialog(u.ctx, wruntime.MessageDialogOptions{
		Type: wruntime.QuestionDialog, Title: "Update maiku",
		Message: fmt.Sprintf("maiku %s is available. Download and install it now? Your current session will continue; the update takes effect next time you open maiku.", update.Version),
		Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No",
	})
	if err != nil || answer != "Yes" {
		return
	}
	ctx, cancel = context.WithTimeout(u.ctx, 5*time.Minute)
	defer cancel()
	if err := u.client.Install(ctx, update); err != nil {
		u.message(wruntime.ErrorDialog, "Could not install the update: "+err.Error())
		return
	}
	u.installed = true
	u.message(wruntime.InfoDialog, "Update installed. Quit and reopen maiku when you're ready. Your current session can continue.")
}

func desktopAutomaticUpdatesEnabled() bool {
	enabled, err := core.AutoUpdateEnabled("")
	return err == nil && enabled && updater.AutomaticEnabled()
}

func (a *App) GetAutoUpdateEnabled() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return core.AutoUpdateEnabled("")
}

func (a *App) SetAutoUpdateEnabled(enabled bool) error {
	a.mu.Lock()
	err := core.PatchGlobalSettings("", map[string]any{"autoUpdate": enabled})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if enabled && a.updates != nil && a.updates.client.Enabled() {
		go a.updates.check(false)
	}
	return nil
}
