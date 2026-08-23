package core

import (
	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/codingagent/core/tools"
)

// ToolOptions configures built-in tools that depend on session settings.
type ToolOptions struct {
	ShellPath          string
	ShellCommandPrefix string
}

func (options ToolOptions) builtinOptions() tools.BuiltinToolOptions {
	return tools.BuiltinToolOptions{
		Bash: tools.BashOptions{
			ShellPath:     options.ShellPath,
			CommandPrefix: options.ShellCommandPrefix,
		},
	}
}

// BuiltinTools returns the default tool set bound to cwd.
func BuiltinTools(cwd string) []agent.AgentTool {
	return BuiltinToolsWithOptions(cwd, ToolOptions{})
}

// BuiltinToolsWithOptions returns the configured default tool set bound to cwd.
func BuiltinToolsWithOptions(cwd string, options ToolOptions) []agent.AgentTool {
	ptrs := tools.CreateBuiltinToolsWithOptions(cwd, nil, options.builtinOptions())
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
	return SelectToolsWithOptions(cwd, allow, exclude, disableAll, ToolOptions{})
}

// SelectToolsWithOptions filters configured built-in tools through an optional
// allowlist and denylist.
func SelectToolsWithOptions(cwd string, allow []string, exclude []string, disableAll bool, options ToolOptions) []agent.AgentTool {
	if disableAll {
		return nil
	}

	names := allow
	if len(names) == 0 {
		// When the caller supplies an allowlist, CreateBuiltinTools uses it
		// exactly, including optional grep/find/ls tools.
		names = tools.DefaultToolNames
	}

	ptrs := tools.CreateBuiltinToolsWithOptions(cwd, names, options.builtinOptions())
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
	return SelectRootToolsWithOptions(cwd, allow, exclude, disableAll, runner, ToolOptions{})
}

// SelectRootToolsWithOptions is SelectRootTools with configured built-in tools.
func SelectRootToolsWithOptions(cwd string, allow []string, exclude []string, disableAll bool, runner *SubagentRunner, options ToolOptions) []agent.AgentTool {
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
		selected = SelectToolsWithOptions(cwd, standardAllow, exclude, false, options)
	}
	if runner != nil && includeSubagent && !excluded[SubagentToolName] {
		selected = append(selected, runner.Tool())
	}
	return selected
}
