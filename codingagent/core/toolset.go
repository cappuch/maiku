package core

import (
	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/codingagent/core/tools"
)

// BuiltinTools returns the default tool set bound to cwd.
func BuiltinTools(cwd string) []agent.AgentTool {
	ptrs := tools.CreateBuiltinTools(cwd, nil)
	selected := make([]agent.AgentTool, 0, len(ptrs))
	for _, tool := range ptrs {
		if tool != nil {
			selected = append(selected, *tool)
		}
	}
	return selected
}

// SelectTools filters built-in tools through an optional allowlist and denylist.
func SelectTools(cwd string, allow []string, exclude []string, disableAll bool) []agent.AgentTool {
	if disableAll {
		return nil
	}

	names := allow
	if len(names) == 0 {
		// Default active set matches TypeScript createCodingTools.
		names = []string{"read", "bash", "edit", "write", "miru"}
		if len(allow) == 0 {
			// When caller passes explicit --tools that includes optional ones,
			// CreateBuiltinTools handles the name list; here we expand defaults
			// then apply exclude.
		}
	}

	// If allowlist is set, use exactly those names (including grep/find/ls).
	if len(allow) > 0 {
		names = allow
	}

	ptrs := tools.CreateBuiltinTools(cwd, names)
	excluded := map[string]bool{}
	for _, name := range exclude {
		excluded[name] = true
	}

	var selected []agent.AgentTool
	for _, tool := range ptrs {
		if tool == nil || excluded[tool.Name] {
			continue
		}
		selected = append(selected, *tool)
	}
	return selected
}
