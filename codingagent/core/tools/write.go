package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

var writeSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "Path to the file to write (relative or absolute)"},
		"content": {"type": "string", "description": "Content to write to the file"}
	},
	"required": ["path", "content"]
}`)

// CreateWriteTool ports coding-agent's write tool (core/tools/write.ts),
// EXECUTE logic only (TUI rendering omitted).
func CreateWriteTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "write",
			Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
			Parameters:  writeSchema,
		},
		Label: "write",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			path, _ := argString(params, "path")
			content, _ := argString(params, "content")

			absolutePath := ResolveToCwd(path, cwd)
			dir := filepath.Dir(absolutePath)

			return WithFileMutationQueue(absolutePath, func() (agent.AgentToolResult, error) {
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				if err := os.MkdirAll(dir, 0o755); err != nil {
					return agent.AgentToolResult{}, err
				}
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				if err := os.WriteFile(absolutePath, []byte(content), 0o644); err != nil {
					return agent.AgentToolResult{}, err
				}
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				return agent.AgentToolResult{
					Content: []ai.ToolResultContent{
						{Type: "text", Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)},
					},
				}, nil
			})
		},
	}
}
