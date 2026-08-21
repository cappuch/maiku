package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

var errOperationAborted = errors.New("operation aborted")

func checkAborted(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return errOperationAborted
	}
	return nil
}

var readSchema = []byte(`{
	"type": "object",
	"properties": {
		"path": {"type": "string", "description": "Path to the file to read (relative or absolute)"},
		"offset": {"type": "number", "description": "Line number to start reading from (1-indexed)"},
		"limit": {"type": "number", "description": "Maximum number of lines to read"}
	},
	"required": ["path"]
}`)

// ReadToolDetails mirrors TS ReadToolDetails.
type ReadToolDetails struct {
	Truncation *TruncationResult
}

// CreateReadTool ports coding-agent's read tool (core/tools/read.ts),
// EXECUTE logic only (TUI rendering omitted).
//
// Deviation from TS: image auto-resizing (image-process.ts /
// image-resize.ts) and the "current model does not support images" note
// (which depends on ExtensionContext.model, unavailable to AgentTool.Execute
// in this Go port) are not implemented. Images are read and base64-encoded
// as-is; unsupported/unconvertible image formats (e.g. BMP) are reported as
// a text note instead of inline image content.
func CreateReadTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: "read",
			Description: fmt.Sprintf(
				"Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
				DefaultMaxLines, DefaultMaxBytes/1024,
			),
			Parameters: readSchema,
		},
		Label: "read",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			path, _ := argString(params, "path")
			offset := argIntPtr(params, "offset")
			limit := argIntPtr(params, "limit")

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			absolutePath := ResolveReadPath(path, cwd)
			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			if _, err := os.Stat(absolutePath); err != nil {
				return agent.AgentToolResult{}, err
			}
			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			mimeType, _ := DetectSupportedImageMimeTypeFromFile(absolutePath)

			if mimeType != "" {
				return readImageResult(absolutePath, mimeType)
			}
			return readTextResult(absolutePath, path, offset, limit)
		},
	}
}

func readImageResult(absolutePath, mimeType string) (agent.AgentToolResult, error) {
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	textNote := fmt.Sprintf("Read image file [%s]", mimeType)
	return agent.AgentToolResult{
		Content: []ai.ToolResultContent{
			{Type: "text", Text: textNote},
			{Type: "image", Data: encoded, MimeType: mimeType},
		},
	}, nil
}

func readTextResult(absolutePath, displayPath string, offset, limit *int) (agent.AgentToolResult, error) {
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	textContent := string(data)
	allLines := strings.Split(textContent, "\n")
	totalFileLines := len(allLines)

	startLine := 0
	if offset != nil && *offset > 0 {
		startLine = *offset - 1
	}
	startLineDisplay := startLine + 1

	if startLine >= len(allLines) {
		if offset != nil {
			return agent.AgentToolResult{}, fmt.Errorf("offset %d is beyond end of file (%d lines total)", *offset, len(allLines))
		}
		return agent.AgentToolResult{}, fmt.Errorf("offset is beyond end of file (%d lines total)", len(allLines))
	}

	var selectedContent string
	var userLimitedLines *int
	if limit != nil {
		endLine := max(min(startLine+*limit, len(allLines)), startLine)
		selectedContent = strings.Join(allLines[startLine:endLine], "\n")
		n := endLine - startLine
		userLimitedLines = &n
	} else {
		selectedContent = strings.Join(allLines[startLine:], "\n")
	}

	truncation := TruncateHead(selectedContent, nil)
	var details *ReadToolDetails
	var outputText string

	switch {
	case truncation.FirstLineExceedsLimit:
		firstLineSize := FormatSize(len(allLines[startLine]))
		outputText = fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLineDisplay, firstLineSize, FormatSize(DefaultMaxBytes), startLineDisplay, displayPath, DefaultMaxBytes,
		)
		details = &ReadToolDetails{Truncation: &truncation}
	case truncation.Truncated:
		endLineDisplay := startLineDisplay + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		outputText = truncation.Content
		if truncation.TruncatedBy == TruncatedByLines {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, nextOffset)
		} else {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, FormatSize(DefaultMaxBytes), nextOffset)
		}
		details = &ReadToolDetails{Truncation: &truncation}
	case userLimitedLines != nil && startLine+*userLimitedLines < len(allLines):
		remaining := len(allLines) - (startLine + *userLimitedLines)
		nextOffset := startLine + *userLimitedLines + 1
		outputText = fmt.Sprintf("%s\n\n[%d more lines in file. Use offset=%d to continue.]", truncation.Content, remaining, nextOffset)
	default:
		outputText = truncation.Content
	}

	result := agent.AgentToolResult{
		Content: []ai.ToolResultContent{{Type: "text", Text: outputText}},
	}
	if details != nil {
		result.Details = details
	}
	return result, nil
}
