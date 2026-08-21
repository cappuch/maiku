package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const lsDefaultLimit = 500

var lsSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "Directory to list (default: current directory)"},
		"limit": {"type": "number", "description": "Maximum number of entries to return (default: 500)"}
	}
}`)

// LsToolDetails mirrors TS LsToolDetails.
type LsToolDetails struct {
	Truncation        *TruncationResult
	EntryLimitReached int
}

// CreateLsTool ports coding-agent's ls tool (core/tools/ls.ts), EXECUTE
// logic only (TUI rendering omitted).
func CreateLsTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: "ls",
			Description: fmt.Sprintf(
				"List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to %d entries or %dKB (whichever is hit first).",
				lsDefaultLimit, DefaultMaxBytes/1024,
			),
			Parameters: lsSchema,
		},
		Label: "ls",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			pathArg := argStringOr(params, "path", ".")
			limitPtr := argIntPtr(params, "limit")

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			dirPath := ResolveToCwd(pathArg, cwd)
			effectiveLimit := lsDefaultLimit
			if limitPtr != nil {
				effectiveLimit = *limitPtr
			}

			if !PathExists(dirPath) {
				return agent.AgentToolResult{}, fmt.Errorf("path not found: %s", dirPath)
			}

			info, err := os.Stat(dirPath)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			if !info.IsDir() {
				return agent.AgentToolResult{}, fmt.Errorf("not a directory: %s", dirPath)
			}

			entries, err := os.ReadDir(dirPath)
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("cannot read directory: %w", err)
			}

			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			sort.Slice(names, func(i, j int) bool {
				return strings.ToLower(names[i]) < strings.ToLower(names[j])
			})

			var results []string
			entryLimitReached := false
			for _, name := range names {
				if len(results) >= effectiveLimit {
					entryLimitReached = true
					break
				}
				fullPath := filepath.Join(dirPath, name)
				st, statErr := os.Stat(fullPath)
				if statErr != nil {
					continue
				}
				suffix := ""
				if st.IsDir() {
					suffix = "/"
				}
				results = append(results, name+suffix)
			}

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			if len(results) == 0 {
				return agent.AgentToolResult{
					Content: []ai.ToolResultContent{{Type: "text", Text: "(empty directory)"}},
				}, nil
			}

			rawOutput := strings.Join(results, "\n")
			truncation := TruncateHead(rawOutput, &TruncationOptions{MaxLines: 1 << 30})
			output := truncation.Content

			details := &LsToolDetails{}
			hasDetails := false
			var notices []string
			if entryLimitReached {
				notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", effectiveLimit, effectiveLimit*2))
				details.EntryLimitReached = effectiveLimit
				hasDetails = true
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
				hasDetails = true
			}
			if len(notices) > 0 {
				output += fmt.Sprintf("\n\n[%s]", strings.Join(notices, ". "))
			}

			result := agent.AgentToolResult{
				Content: []ai.ToolResultContent{{Type: "text", Text: output}},
			}
			if hasDetails {
				result.Details = details
			}
			return result, nil
		},
	}
}
