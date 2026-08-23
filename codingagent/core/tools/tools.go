package tools

import "github.com/mikus/maiku/agent"

// ToolName identifies a builtin coding-agent tool.
type ToolName string

const (
	ToolRead      ToolName = "read"
	ToolBash      ToolName = "bash"
	ToolEdit      ToolName = "edit"
	ToolWrite     ToolName = "write"
	ToolGrep      ToolName = "grep"
	ToolFind      ToolName = "find"
	ToolLs        ToolName = "ls"
	ToolMiru      ToolName = "miru"
	ToolWebSearch ToolName = "web_search"
	ToolCurl      ToolName = "curl"
)

// BuiltinToolOptions configures built-in tools that need runtime settings.
type BuiltinToolOptions struct {
	Bash BashOptions
}

// DefaultToolNames are the tools included in coding-agent's default toolset.
var DefaultToolNames = []string{
	string(ToolRead),
	string(ToolBash),
	string(ToolEdit),
	string(ToolWrite),
	string(ToolMiru),
	string(ToolWebSearch),
	string(ToolCurl),
}

func createBuiltinTool(name string, cwd string, options BuiltinToolOptions) *agent.AgentTool {
	switch ToolName(name) {
	case ToolRead:
		return CreateReadTool(cwd)
	case ToolBash:
		return CreateBashToolWithOptions(cwd, options.Bash)
	case ToolEdit:
		return CreateEditTool(cwd)
	case ToolWrite:
		return CreateWriteTool(cwd)
	case ToolGrep:
		return CreateGrepTool(cwd)
	case ToolFind:
		return CreateFindTool(cwd)
	case ToolLs:
		return CreateLsTool(cwd)
	case ToolMiru:
		return CreateMiruTool(cwd)
	case ToolWebSearch:
		return CreateWebSearchTool()
	case ToolCurl:
		return CreateCurlTool()
	default:
		return nil
	}
}

// CreateBuiltinTools builds coding-agent's builtin tools for the given
// working directory.
//
// Mirrors TS core/tools/index.ts: if names is empty, the default toolset is
// returned, matching createCodingTools. Passing
// names explicitly (e.g. including "grep", "find", "ls") mirrors
// createAllTools/createReadOnlyTools-style selection. Unknown names are
// ignored.
func CreateBuiltinTools(cwd string, names []string) []*agent.AgentTool {
	return CreateBuiltinToolsWithOptions(cwd, names, BuiltinToolOptions{})
}

// CreateBuiltinToolsWithOptions is CreateBuiltinTools with per-tool runtime
// configuration, including the platform shell override and command prefix.
func CreateBuiltinToolsWithOptions(cwd string, names []string, options BuiltinToolOptions) []*agent.AgentTool {
	if len(names) == 0 {
		names = DefaultToolNames
	}
	selected := make([]*agent.AgentTool, 0, len(names))
	for _, name := range names {
		if tool := createBuiltinTool(name, cwd, options); tool != nil {
			selected = append(selected, tool)
		}
	}
	return selected
}
