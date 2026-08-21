package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

var editSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "Path to the file to edit (relative or absolute)"},
		"edits": {
			"type": "array",
			"description": "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead.",
			"items": {
				"type": "object",
				"properties": {
					"oldText": {"type": "string", "description": "Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},
					"newText": {"type": "string", "description": "Replacement text for this targeted edit."}
				},
				"required": ["oldText", "newText"]
			}
		}
	},
	"required": ["path", "edits"]
}`)

// EditToolDetails mirrors TS EditToolDetails.
type EditToolDetails struct {
	// Diff is a display-oriented diff of the changes made.
	Diff string `json:"diff"`
	// Patch is a standard unified patch of the changes made.
	Patch string `json:"patch"`
	// FirstChangedLine is the line number of the first change in the new
	// file (for editor navigation). 0 means "no changes" (should not occur
	// on success).
	FirstChangedLine int `json:"firstChangedLine"`
}

// prepareEditArguments mirrors TS prepareEditArguments: some models send
// edits as a JSON-encoded string instead of an array, and some send a
// legacy single {oldText, newText} pair instead of an edits[] array.
func prepareEditArguments(args map[string]any) (map[string]any, error) {
	if args == nil {
		return args, nil
	}
	result := make(map[string]any, len(args))
	maps.Copy(result, args)

	if editsStr, ok := result["edits"].(string); ok {
		var parsed []any
		if err := json.Unmarshal([]byte(editsStr), &parsed); err == nil {
			result["edits"] = parsed
		}
	}

	oldText, hasOld := result["oldText"].(string)
	newText, hasNew := result["newText"].(string)
	if hasOld && hasNew {
		var edits []any
		if arr, ok := result["edits"].([]any); ok {
			edits = append(edits, arr...)
		}
		edits = append(edits, map[string]any{"oldText": oldText, "newText": newText})
		delete(result, "oldText")
		delete(result, "newText")
		result["edits"] = edits
	}

	return result, nil
}

func validateEditInput(args map[string]any) (path string, edits []Edit, err error) {
	path, _ = argString(args, "path")
	rawEdits, ok := args["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return "", nil, errors.New("edit tool input is invalid: edits must contain at least one replacement")
	}
	edits = make([]Edit, 0, len(rawEdits))
	for _, re := range rawEdits {
		m, ok := re.(map[string]any)
		if !ok {
			return "", nil, errors.New("edit tool input is invalid: edits must contain at least one replacement")
		}
		oldText, _ := m["oldText"].(string)
		newText, _ := m["newText"].(string)
		edits = append(edits, Edit{OldText: oldText, NewText: newText})
	}
	return path, edits, nil
}

func editAccessErrorMessage(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "Error code: ENOENT"
	}
	if errors.Is(err, os.ErrPermission) {
		return "Error code: EACCES"
	}
	return err.Error()
}

// CreateEditTool ports coding-agent's edit tool (core/tools/edit.ts and
// edit-diff.ts), EXECUTE logic only (TUI rendering / diff preview omitted).
func CreateEditTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "edit",
			Description: "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits. Do not include large unchanged regions just to connect distant changes.",
			Parameters:  editSchema,
		},
		Label:            "edit",
		PrepareArguments: prepareEditArguments,
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			path, edits, err := validateEditInput(params)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			absolutePath := ResolveToCwd(path, cwd)

			return WithFileMutationQueue(absolutePath, func() (agent.AgentToolResult, error) {
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				f, err := os.OpenFile(absolutePath, os.O_RDWR, 0)
				if err != nil {
					if aerr := checkAborted(ctx); aerr != nil {
						return agent.AgentToolResult{}, aerr
					}
					return agent.AgentToolResult{}, fmt.Errorf("could not edit file %s: %s", path, editAccessErrorMessage(err))
				}
				if err := f.Close(); err != nil {
					return agent.AgentToolResult{}, fmt.Errorf("could not edit file %s: %w", path, err)
				}
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				rawContentBytes, err := os.ReadFile(absolutePath)
				if err != nil {
					return agent.AgentToolResult{}, err
				}
				rawContent := string(rawContentBytes)
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				bom, content := StripBom(rawContent)
				originalEnding := DetectLineEnding(content)
				normalizedContent := NormalizeToLF(content)
				applied, err := ApplyEditsToNormalizedContent(normalizedContent, edits, path)
				if err != nil {
					return agent.AgentToolResult{}, err
				}
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				finalContent := bom + RestoreLineEndings(applied.NewContent, originalEnding)
				if err := os.WriteFile(absolutePath, []byte(finalContent), 0o644); err != nil {
					return agent.AgentToolResult{}, err
				}
				if err := checkAborted(ctx); err != nil {
					return agent.AgentToolResult{}, err
				}

				diffResult := GenerateDiffString(applied.BaseContent, applied.NewContent, 4)
				patch := GenerateUnifiedPatch(path, applied.BaseContent, applied.NewContent, 4)

				return agent.AgentToolResult{
					Content: []ai.ToolResultContent{
						{Type: "text", Text: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path)},
					},
					Details: &EditToolDetails{
						Diff:             diffResult.Diff,
						Patch:            patch,
						FirstChangedLine: diffResult.FirstChangedLine,
					},
				}, nil
			})
		},
	}
}
