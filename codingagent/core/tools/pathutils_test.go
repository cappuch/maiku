package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPathAcceptsBothTildeSeparators(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "nested", "file.txt")
	for _, input := range []string{"~/nested/file.txt", `~\nested\file.txt`} {
		if got := ExpandPath(input); got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", input, got, want)
		}
	}
}
