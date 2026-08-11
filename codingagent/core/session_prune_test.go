package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikus/maiku/ai"
)

func writeSessionFile(t *testing.T, path, id string, withMessage bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	header := `{"type":"session","version":1,"id":"` + id + `","timestamp":"2024-01-01T00:00:00.000Z","cwd":"/tmp"}`
	content := header + "\n"
	if withMessage {
		msg := ai.Message{
			Role: "user",
			UserContent: []ai.TextContent{
				{Type: "text", Text: "hello"},
			},
		}
		encoded, err := EncodeMessage(msg)
		if err != nil {
			t.Fatal(err)
		}
		content += `{"type":"message","id":"` + ai.UUIDv7() + `","timestamp":"2024-01-01T00:00:00.000Z","message":` + string(encoded) + "}\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPruneEmptySessions(t *testing.T) {
	dir := t.TempDir()

	emptyRoot := filepath.Join(dir, "empty.jsonl")
	writeSessionFile(t, emptyRoot, "empty-root", false)

	fullRoot := filepath.Join(dir, "full.jsonl")
	writeSessionFile(t, fullRoot, "full-root", true)

	sub := filepath.Join(dir, "--project--")
	emptySub := filepath.Join(sub, "empty-sub.jsonl")
	writeSessionFile(t, emptySub, "empty-sub", false)
	fullSub := filepath.Join(sub, "full-sub.jsonl")
	writeSessionFile(t, fullSub, "full-sub", true)

	// Not a session file (no header) — must survive.
	other := filepath.Join(dir, "notes.jsonl")
	if err := os.WriteFile(other, []byte(`{"type":"message","message":{}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed := PruneEmptySessions(dir, map[string]bool{emptySub: true})

	if removed != 1 {
		t.Fatalf("expected 1 removal, got %d", removed)
	}
	if _, err := os.Stat(emptyRoot); !os.IsNotExist(err) {
		t.Errorf("empty session at root should be removed, err=%v", err)
	}
	if _, err := os.Stat(emptySub); err != nil {
		t.Errorf("empty session in keep set should survive, err=%v", err)
	}
	for _, keep := range []string{fullRoot, fullSub, other} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s should survive, err=%v", keep, err)
		}
	}
}
