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
		// When the caller supplies an allowlist, CreateBuiltinTools uses it
		// exactly, including optional grep/find/ls tools.
		names = tools.DefaultToolNames
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

// SelectRootTools extends the normal tool registry with a root-owned
// subagent tool. SelectTools deliberately remains unaware of subagents, so it
// is safe to use for child sessions and recursive delegation is impossible.
//
// With no allowlist, subagent is enabled alongside the normal defaults. With
// an explicit allowlist it must be named explicitly. The existing denylist
// and disableAll behavior applies to it as well.
func SelectRootTools(cwd string, allow []string, exclude []string, disableAll bool, runner *SubagentRunner) []agent.AgentTool {
	if disableAll {
		return nil
	}

	excluded := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		excluded[name] = true
	}

	includeSubagent := len(allow) == 0
	standardAllow := allow
	if len(allow) > 0 {
		standardAllow = make([]string, 0, len(allow))
		for _, name := range allow {
			if name == SubagentToolName {
				includeSubagent = true
				continue
			}
			standardAllow = append(standardAllow, name)
		}
	}

	var selected []agent.AgentTool
	if len(allow) == 0 || len(standardAllow) > 0 {
		selected = SelectTools(cwd, standardAllow, exclude, false)
	}
	if runner != nil && includeSubagent && !excluded[SubagentToolName] {
		selected = append(selected, runner.Tool())
	}
	return selected
}
