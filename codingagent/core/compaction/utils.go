// Package compaction summarizes older conversation history when a session
// approaches the model's context window. It is a reduced port of the
// TypeScript coding agent's compaction module: cut-point selection and
// LLM summarization are ported, session-entry bookkeeping and branch
// summarization are not.
package compaction

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

// ToolResultMaxChars caps a single tool result inside a serialized
// conversation; summaries do not need the full text.
const ToolResultMaxChars = 2000

// FileOperations tracks the files touched by the messages being summarized.
type FileOperations struct {
	Read    map[string]bool
	Written map[string]bool
	Edited  map[string]bool
}

// NewFileOperations returns an empty tracker.
func NewFileOperations() FileOperations {
	return FileOperations{
		Read:    map[string]bool{},
		Written: map[string]bool{},
		Edited:  map[string]bool{},
	}
}

// ExtractFileOps records read/write/edit tool calls from an assistant message.
func ExtractFileOps(message agent.AgentMessage, ops FileOperations) {
	if message.Role != "assistant" {
		return
	}
	for _, block := range message.AssistantContent {
		if block.Type != "toolCall" || block.Arguments == nil {
			continue
		}
		path, ok := block.Arguments["path"].(string)
		if !ok || path == "" {
			continue
		}
		switch block.Name {
		case "read":
			ops.Read[path] = true
		case "write":
			ops.Written[path] = true
		case "edit":
			ops.Edited[path] = true
		}
	}
}

// ComputeFileLists splits tracked files into read-only and modified sets.
func ComputeFileLists(ops FileOperations) (readFiles, modifiedFiles []string) {
	modified := map[string]bool{}
	for path := range ops.Edited {
		modified[path] = true
	}
	for path := range ops.Written {
		modified[path] = true
	}
	for path := range ops.Read {
		if !modified[path] {
			readFiles = append(readFiles, path)
		}
	}
	for path := range modified {
		modifiedFiles = append(modifiedFiles, path)
	}
	sort.Strings(readFiles)
	sort.Strings(modifiedFiles)
	return readFiles, modifiedFiles
}

// FormatFileOperations renders the file lists appended to a summary.
func FormatFileOperations(readFiles, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(readFiles, "\n")+"\n</read-files>")
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(modifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

func truncateForSummary(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return fmt.Sprintf("%s\n\n[... %d more characters truncated]", text[:maxChars], len(text)-maxChars)
}

// UserText concatenates the text blocks of a user message's content.
// ai.ContentText only understands string and decoded-JSON block maps, so this
// also handles the typed ai.TextContent blocks built in-process.
func UserText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []ai.TextContent:
		var out strings.Builder
		for _, block := range value {
			out.WriteString(block.Text)
		}
		return out.String()
	case []any:
		var out strings.Builder
		for _, item := range value {
			switch block := item.(type) {
			case ai.TextContent:
				out.WriteString(block.Text)
			case map[string]any:
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok {
						out.WriteString(text)
					}
				}
			}
		}
		return out.String()
	default:
		return ""
	}
}

func toolResultText(message agent.AgentMessage) string {
	var out strings.Builder
	for _, block := range message.ToolContent {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}

// SerializeConversation renders messages as plain text so the summarizing
// model treats them as data instead of a conversation to continue.
func SerializeConversation(messages []agent.AgentMessage) string {
	var parts []string

	for _, message := range messages {
		switch message.Role {
		case "user":
			if text := UserText(message.UserContent); text != "" {
				parts = append(parts, "[User]: "+text)
			}

		case "assistant":
			var thinking, toolCalls []string
			hasText := false
			for _, block := range message.AssistantContent {
				switch block.Type {
				case "thinking":
					thinking = append(thinking, block.Thinking)
				case "text":
					hasText = true
				case "toolCall":
					toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", block.Name, formatToolArgs(block.Arguments)))
				}
			}
			if len(thinking) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinking, "\n"))
			}
			if hasText {
				if assistant, ok := message.AsAssistant(); ok {
					parts = append(parts, "[Assistant]: "+ai.AssistantText(assistant))
				}
			}
			if len(toolCalls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
			}

		case "toolResult":
			if text := toolResultText(message); text != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(text, ToolResultMaxChars))
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

func formatToolArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded, err := json.Marshal(args[key])
		if err != nil {
			continue
		}
		pairs = append(pairs, key+"="+string(encoded))
	}
	return strings.Join(pairs, ", ")
}

// SummarizationSystemPrompt keeps the summarizing model from continuing the
// conversation it is given.
const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`
