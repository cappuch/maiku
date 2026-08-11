package tools

import "github.com/mikus/maiku/agent"

// ToolName identifies a builtin coding-agent tool.
type ToolName string

const (
	ToolRead  ToolName = "read"
	ToolBash  ToolName = "bash"
	ToolEdit  ToolName = "edit"
	ToolWrite ToolName = "write"
	ToolGrep  ToolName = "grep"
	ToolFind  ToolName = "find"
	ToolLs    ToolName = "ls"
	ToolMiru  ToolName = "miru"
)

// DefaultToolNames are the tools included in coding-agent's default toolset.
var DefaultToolNames = []string{string(ToolRead), string(ToolBash), string(ToolEdit), string(ToolWrite), string(ToolMiru)}

func createBuiltinTool(name string, cwd string) *agent.AgentTool {
	switch ToolName(name) {
	case ToolRead:
		return CreateReadTool(cwd)
	case ToolBash:
		return CreateBashTool(cwd)
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
	default:
		return nil
	}
}

// CreateBuiltinTools builds coding-agent's builtin tools for the given
// working directory.
//
// Mirrors TS core/tools/index.ts: if names is empty, the default toolset
// (read, bash, edit, write) is returned, matching createCodingTools. Passing
// names explicitly (e.g. including "grep", "find", "ls") mirrors
// createAllTools/createReadOnlyTools-style selection. Unknown names are
// ignored.
func CreateBuiltinTools(cwd string, names []string) []*agent.AgentTool {
	if len(names) == 0 {
		names = DefaultToolNames
	}
	tools := make([]*agent.AgentTool, 0, len(names))
	for _, name := range names {
		if t := createBuiltinTool(name, cwd); t != nil {
			tools = append(tools, t)
		}
	}
	return tools
}
