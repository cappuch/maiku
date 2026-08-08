package core

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikus/maiku/ai"
)

// SessionSummary is a lightweight listing entry for the desktop/CLI UI.
type SessionSummary struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Preview   string `json:"preview"`
	ModTime   string `json:"modTime"`
	Name      string `json:"name"` // user-assigned display name, optional
}

// ListSessionSummaries scans dir (non-recursively) and also one level of
// cwd-scoped subdirectories for session JSONL files.
func ListSessionSummaries(dir string) []SessionSummary {
	var out []SessionSummary
	seen := map[string]bool{}
	names := LoadSessionNames()

	addFile := func(path string) {
		if seen[path] {
			return
		}
		header, err := readSessionHeader(path)
		if err != nil {
			return
		}
		info, _ := os.Stat(path)
		mod := ""
		if info != nil {
			mod = info.ModTime().UTC().Format(time.RFC3339)
		}
		seen[path] = true
		out = append(out, SessionSummary{
			ID:        header.ID,
			Path:      path,
			Timestamp: header.Timestamp,
			Cwd:       header.Cwd,
			Preview:   firstUserPreview(path),
			ModTime:   mod,
			Name:      names[path],
		})
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			sub, err := os.ReadDir(full)
			if err != nil {
				continue
			}
			for _, s := range sub {
				if !s.IsDir() && strings.HasSuffix(s.Name(), ".jsonl") {
					addFile(filepath.Join(full, s.Name()))
				}
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".jsonl") {
			addFile(full)
		}
	}

	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ModTime > out[i].ModTime {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func firstUserPreview(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry SessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "message" || len(entry.Message) == 0 {
			continue
		}
		msg, err := DecodeMessage(entry.Message)
		if err != nil || msg.Role != "user" {
			continue
		}
		text := strings.TrimSpace(ai.ContentText(msg.UserContent))
		if text == "" {
			continue
		}
		if len(text) > 80 {
			return text[:80] + "…"
		}
		return text
	}
	return ""
}
