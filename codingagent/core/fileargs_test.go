package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAtMentions(t *testing.T) {
	got := ExtractAtMentions(`look at @src/main.go and @"docs/my notes.md" plus @'a b.txt'`)
	want := []string{"src/main.go", "docs/my notes.md", "a b.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// Dedupes.
	got = ExtractAtMentions("@a.go @a.go")
	if len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("dedupe failed: %v", got)
	}

	// An @ embedded in pasted text is not a file mention. In particular, a
	// versioned path must not be reduced to "v4" and resolved under cwd.
	got = ExtractAtMentions("/tmp/pkg@v4 user@example.com github.com/acme/lib@v4/file.go")
	if len(got) != 0 {
		t.Fatalf("embedded @ parsed as mentions: %v", got)
	}

	// Opening delimiters are valid mention boundaries.
	got = ExtractAtMentions("see (@a.go) and [@b.go]")
	want = []string{"a.go", "b.go"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delimited mentions: got %v, want %v", got, want)
	}
}

func TestProcessFileArgumentsTextAndImage(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(textPath, []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal 1x1 PNG
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	)
	if err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(dir, "dot.png")
	if err := os.WriteFile(imgPath, png, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ProcessFileArguments(dir, []string{"notes.md", "dot.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(got.Text, "<file path=\"notes.md\">", "# hello", "<file path=\"dot.png\"></file>") {
		t.Fatalf("unexpected text: %q", got.Text)
	}
	if len(got.Images) != 1 || got.Images[0].MimeType != "image/png" {
		t.Fatalf("unexpected images: %+v", got.Images)
	}
}

func TestExpandAtMentions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ExpandAtMentions(dir, "please review @a.go carefully")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(got.Text, "<file path=\"a.go\">", "package a", "please review @a.go carefully") {
		t.Fatalf("unexpected: %q", got.Text)
	}
}

func TestExpandAtMentionsLeavesUnresolvedTextAlone(t *testing.T) {
	dir := t.TempDir()
	messages := []string{
		"upgrade to @v4",
		"inspect /outside/cache/pkg@v4/file.go",
		"email user@example.com",
	}
	for _, message := range messages {
		got, err := ExpandAtMentions(dir, message)
		if err != nil {
			t.Fatalf("ExpandAtMentions(%q): %v", message, err)
		}
		if got.Text != message || len(got.Images) != 0 {
			t.Fatalf("ExpandAtMentions(%q) = %+v", message, got)
		}
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
