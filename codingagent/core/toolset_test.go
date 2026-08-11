package core

import (
	"testing"

	"github.com/mikus/maiku/ai"
)

func TestBuiltinToolSchemasValidate(t *testing.T) {
	cases := map[string]map[string]any{
		"read":  {"path": "main.go"},
		"bash":  {"command": "echo hi"},
		"edit":  {"path": "main.go", "edits": []any{map[string]any{"oldText": "a", "newText": "b"}}},
		"write": {"path": "main.go", "content": "hi"},
		"miru":  {"query": "authentication flow"},
	}

	tools := BuiltinTools(t.TempDir())
	if len(tools) == 0 {
		t.Fatal("BuiltinTools returned no tools")
	}

	for _, tool := range tools {
		args, ok := cases[tool.Name]
		if !ok {
			t.Errorf("unexpected built-in tool %q", tool.Name)
			continue
		}
		if _, err := ai.ValidateToolArguments(tool.Tool, args); err != nil {
			t.Errorf("%s: schema rejected valid arguments: %v", tool.Name, err)
		}
	}
}
