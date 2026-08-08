package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikus/maiku/codingagent"
)

// Session display names live in a small sidecar file rather than inside the
// JSONL transcripts, so renaming a session never risks corrupting it.
const sessionNamesFile = "session-names.json"

func sessionNamesPath() string {
	return filepath.Join(codingagent.GetAgentDir(), sessionNamesFile)
}

// LoadSessionNames returns a map of session file path -> display name. Stale
// entries whose files no longer exist are dropped.
func LoadSessionNames() map[string]string {
	data, err := os.ReadFile(sessionNamesPath())
	if err != nil {
		return nil
	}
	names := map[string]string{}
	if err := json.Unmarshal(data, &names); err != nil {
		return nil
	}
	out := map[string]string{}
	for path, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out[path] = name
	}
	return out
}

// SaveSessionName sets the display name for a session file, or clears it when
// name is empty.
func SaveSessionName(path, name string) error {
	path = filepath.Clean(path)
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		name = name[:80]
	}

	names := LoadSessionNames()
	if names == nil {
		names = map[string]string{}
	}
	if name == "" {
		delete(names, path)
	} else {
		names[path] = name
	}

	filePath := sessionNamesPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0o644)
}
