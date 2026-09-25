package core

import (
	"os"
	"testing"
)

func TestAutoUpdatePreference(t *testing.T) {
	dir := t.TempDir()
	if enabled, err := AutoUpdateEnabled(dir); err != nil || !enabled {
		t.Fatalf("missing preference should default on: enabled=%v error=%v", enabled, err)
	}
	if err := PatchGlobalSettings(dir, map[string]any{"defaultModel": "keep-me", "autoUpdate": false}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoUpdateEnabled(dir); err != nil || enabled {
		t.Fatalf("saved preference should stay off: enabled=%v error=%v", enabled, err)
	}
	if err := PatchGlobalSettings(dir, map[string]any{"autoUpdate": true}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoUpdateEnabled(dir); err != nil || !enabled {
		t.Fatalf("enabled=%v error=%v", enabled, err)
	}
	raw, err := readSettingsFile(GlobalSettingsPath(dir))
	if err != nil || raw["defaultModel"] != "keep-me" {
		t.Fatalf("unrelated settings changed: %v, %v", raw, err)
	}
	if err := PatchGlobalSettings(dir, map[string]any{"autoUpdate": "false"}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoUpdateEnabled(dir); err == nil || enabled {
		t.Fatal("invalid preference must not enable updates")
	}
	if err := os.WriteFile(GlobalSettingsPath(dir), []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoUpdateEnabled(dir); err == nil || enabled {
		t.Fatal("corrupt settings must not enable updates")
	}
}
