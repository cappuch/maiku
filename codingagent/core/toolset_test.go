package core

import (
	"strings"
	"testing"

	"github.com/mikus/maiku/ai"
)

func TestBuiltinToolSchemasValidate(t *testing.T) {
	cases := map[string]map[string]any{
		"read":       {"path": "main.go"},
		"bash":       {"command": "echo hi"},
		"edit":       {"path": "main.go", "edits": []any{map[string]any{"oldText": "a", "newText": "b"}}},
		"write":      {"path": "main.go", "content": "hi"},
		"miru":       {"query": "authentication flow"},
		"web_search": {"query": "Go context cancellation", "max_results": 5},
		"curl":       {"url": "https://example.com"},
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
		delete(cases, tool.Name)
	}
	if len(cases) != 0 {
		t.Errorf("default toolset is missing tools: %v", cases)
	}

	prompt := BuildSystemPrompt(BuildSystemPromptOptions{Cwd: t.TempDir()})
	for _, name := range []string{"miru", "web_search", "curl"} {
		if !strings.Contains(prompt, "- "+name+":") {
			t.Errorf("default system prompt does not advertise %q", name)
		}
	}
}
