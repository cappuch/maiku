package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikus/maiku/codingagent"
)

const maxRecentDirs = 5

func recentDirsPath() string {
	return filepath.Join(codingagent.GetAgentDir(), "recent-dirs.json")
}

func loadRecentDirs() []string {
	data, err := os.ReadFile(recentDirsPath())
	if err != nil {
		return nil
	}
	var dirs []string
	if err := json.Unmarshal(data, &dirs); err != nil {
		return nil
	}
	out := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			continue
		}
		seen[d] = true
		out = append(out, d)
		if len(out) >= maxRecentDirs {
			break
		}
	}
	return out
}

func saveRecentDirs(dirs []string) {
	path := recentDirsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(dirs, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func (a *App) rememberDir(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rememberDirLocked(path)
}

func (a *App) rememberDirLocked(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	next := make([]string, 0, maxRecentDirs)
	next = append(next, path)
	for _, d := range a.recentDirs {
		if d == path {
			continue
		}
		next = append(next, d)
		if len(next) >= maxRecentDirs {
			break
		}
	}
	a.recentDirs = next
	saveRecentDirs(next)
}
