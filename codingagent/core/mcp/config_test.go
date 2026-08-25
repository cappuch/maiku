package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergeProjectOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agent")
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(cwd, ".maiku"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	global := File{MCPServers: map[string]ServerConfig{
		"shared": {Command: "global-cmd", Args: []string{"a"}},
		"only-global": {Command: "g"},
	}}
	writeTestFile(t, GlobalPath(agentDir), global)

	project := File{MCPServers: map[string]ServerConfig{
		"shared": {Command: "project-cmd", Args: []string{"b"}, Env: map[string]string{"K": "V"}},
		"only-project": {Command: "p"},
	}}
	writeTestFile(t, ProjectPath(cwd), project)

	loaded := Load(cwd, agentDir)
	if loaded.Servers["shared"].Command != "project-cmd" {
		t.Fatalf("shared command = %q, want project-cmd", loaded.Servers["shared"].Command)
	}
	if loaded.Servers["only-global"].Command != "g" {
		t.Fatalf("missing only-global")
	}
	if loaded.Servers["only-project"].Command != "p" {
		t.Fatalf("missing only-project")
	}
}

func TestUpsertAndRemoveGlobal(t *testing.T) {
	agentDir := t.TempDir()
	cfg := ServerConfig{Command: "npx", Args: []string{"-y", "demo"}, Env: map[string]string{"A": "1"}}
	if err := UpsertGlobal(agentDir, "demo", cfg); err != nil {
		t.Fatal(err)
	}
	loaded := Load("", agentDir)
	got, ok := loaded.Servers["demo"]
	if !ok {
		t.Fatal("demo missing after upsert")
	}
	if got.Command != "npx" || len(got.Args) != 2 || got.Env["A"] != "1" {
		t.Fatalf("unexpected config: %+v", got)
	}

	if err := SetDisabledGlobal(agentDir, "demo", true); err != nil {
		t.Fatal(err)
	}
	loaded = Load("", agentDir)
	if !loaded.Servers["demo"].Disabled {
		t.Fatal("expected disabled")
	}

	if err := RemoveGlobal(agentDir, "demo"); err != nil {
		t.Fatal(err)
	}
	loaded = Load("", agentDir)
	if _, ok := loaded.Servers["demo"]; ok {
		t.Fatal("demo still present after remove")
	}
}

func TestServerConfigEnabled(t *testing.T) {
	cases := []struct {
		cfg  ServerConfig
		want bool
	}{
		{ServerConfig{Command: "x"}, true},
		{ServerConfig{Command: "x", Type: "stdio"}, true},
		{ServerConfig{Command: "x", Disabled: true}, false},
		{ServerConfig{Command: "", Type: "stdio"}, false},
		{ServerConfig{Command: "x", Type: "sse"}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.Enabled(); got != tc.want {
			t.Fatalf("Enabled(%+v)=%v want %v", tc.cfg, got, tc.want)
		}
	}
}

func writeTestFile(t *testing.T, path string, file File) {
	t.Helper()
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
