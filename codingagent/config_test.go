package codingagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTildePathAcceptsBothSeparators(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "nested", "file.txt")
	for _, input := range []string{"~/nested/file.txt", `~\nested\file.txt`} {
		if got := ExpandTildePath(input); got != want {
			t.Errorf("ExpandTildePath(%q) = %q, want %q", input, got, want)
		}
	}
}
