package computer

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/cappuch/maiku/agent"
	"github.com/cappuch/maiku/ai"
)

const ToolName = "computer"

var toolSchema = []byte(`{
	"type": "object",
	"properties": {
		"action": {"type": "string", "enum": ["screenshot", "click", "move", "drag", "type", "key", "scroll", "wait", "batch"], "description": "What to do on the desktop"},
		"x": {"type": "integer", "description": "X pixel from the left of the latest screenshot"},
		"y": {"type": "integer", "description": "Y pixel from the top of the latest screenshot"},
		"x1": {"type": "integer"}, "y1": {"type": "integer"}, "x2": {"type": "integer"}, "y2": {"type": "integer"},
		"dx": {"type": "integer", "description": "Horizontal scroll lines. Positive scrolls right"},
		"dy": {"type": "integer", "description": "Vertical scroll lines. Positive scrolls down"},
		"button": {"type": "string", "enum": ["left", "right", "middle"]},
		"clicks": {"type": "integer", "description": "1 for a single click, 2 for a double click"},
		"text": {"type": "string", "description": "Text to type at the current keyboard focus. Newlines press Enter"},
		"keys": {"type": "string", "description": "Key combo joined with +, e.g. enter, cmd+space, cmd+shift+t, escape"},
		"seconds": {"type": "integer", "description": "Seconds to wait, 1-10"},
		"actions": {
			"type": "array",
			"description": "For action=batch. Each item has action plus that action's fields, and optional delay_ms (0-10000) to pause after it",
			"items": {
				"type": "object",
				"properties": {
					"action": {"type": "string", "enum": ["click", "move", "drag", "type", "key", "scroll", "wait"]},
					"x": {"type": "integer"}, "y": {"type": "integer"},
					"x1": {"type": "integer"}, "y1": {"type": "integer"}, "x2": {"type": "integer"}, "y2": {"type": "integer"},
					"dx": {"type": "integer"}, "dy": {"type": "integer"},
					"button": {"type": "string"}, "clicks": {"type": "integer"},
					"text": {"type": "string"}, "keys": {"type": "string"},
					"seconds": {"type": "integer"}, "delay_ms": {"type": "integer"}
				},
				"required": ["action"]
			}
		}
	},
	"required": ["action"]
}`)

// Tool drives the macOS desktop and returns a screenshot after each action.
// The desktop app registers it. The CLI does not import this package, so the
// tool is left out of CLI builds.
func Tool() *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: ToolName,
			Description: "Control the user's macOS desktop. Coordinates are pixels in the screenshot this tool returns (top-left origin, 1:1 with the screen). " +
				"Start with action=screenshot. Then click the center of UI elements, type only after focusing a field, and prefer reliable shortcuts (cmd+space opens Spotlight). " +
				"Use batch for a short sequence you are sure about (click a field, type, press enter) and put delay_ms on steps that need the UI to catch up. " +
				"Every call returns a new screenshot — read it before the next action.",
			Parameters: toolSchema,
		},
		Label:         ToolName,
		ExecutionMode: agent.ToolExecutionSequential,
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			if ctx != nil && ctx.Err() != nil {
				return agent.AgentToolResult{}, errors.New("operation aborted")
			}
			action, _ := params["action"].(string)
			result, err := Act(ctx, action, params)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			return agent.AgentToolResult{
				Content: []ai.ToolResultContent{
					{Type: "text", Text: result.Text},
					{Type: "image", MimeType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(result.JPEG)},
				},
				Details: map[string]any{"width": result.Width, "height": result.Height},
			}, nil
		},
	}
}
