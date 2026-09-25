package bedrock

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cappuch/maiku/ai"
)

type object = map[string]any
type message struct {
	Role    string   `json:"role"`
	Content []object `json:"content"`
}

var invalidToolID = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

var claudeVersion = regexp.MustCompile(`anthropic\.claude-(?:sonnet|opus)-([0-9]+)(?:-([0-9])(?:-|$))?`)

func adaptiveThinking(id string) bool {
	v := claudeVersion.FindStringSubmatch(id)
	if len(v) == 0 {
		return strings.Contains(id, "anthropic.claude-fable-") || strings.Contains(id, "anthropic.claude-mythos-")
	}
	major, _ := strconv.Atoi(v[1])
	minor, _ := strconv.Atoi(v[2])
	return major > 4 || (major == 4 && minor >= 6)
}

func toolID(id string) string {
	id = invalidToolID.ReplaceAllString(id, "_")
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

func imageBlock(data, mime string) (object, error) {
	format := strings.TrimPrefix(mime, "image/")
	if format != "png" && format != "jpeg" && format != "gif" && format != "webp" {
		return nil, fmt.Errorf("unsupported Bedrock image type %q", mime)
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return nil, fmt.Errorf("invalid Bedrock image: %w", err)
	}
	return object{"image": object{"format": format, "source": object{"bytes": data}}}, nil
}

func userBlocks(content any) ([]object, error) {
	if text, ok := content.(string); ok {
		return []object{{"text": text}}, nil
	}
	// Normalize typed slices and JSON-decoded history through the shared wire shape.
	data, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	var blocks []ai.ToolResultContent
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil, fmt.Errorf("invalid user content: %w", err)
	}
	return contentBlocks(blocks)
}

func contentBlocks(blocks []ai.ToolResultContent) ([]object, error) {
	var out []object
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				out = append(out, object{"text": b.Text})
			}
		case "image":
			image, err := imageBlock(b.Data, b.MimeType)
			if err != nil {
				return nil, err
			}
			out = append(out, image)
		default:
			return nil, fmt.Errorf("unsupported Bedrock content type %q", b.Type)
		}
	}
	return out, nil
}

func buildRequest(model ai.Model, ctx ai.Context, opts *ai.SimpleStreamOptions) (object, error) {
	messages := []message{}
	for _, m := range ctx.Messages {
		role := m.Role
		var blocks []object
		var err error
		switch role {
		case "user":
			blocks, err = userBlocks(m.UserContent)
		case "assistant":
			for _, b := range m.AssistantContent {
				switch b.Type {
				case "text":
					if b.Text != "" {
						blocks = append(blocks, object{"text": b.Text})
					}
				case "toolCall":
					args := b.Arguments
					if args == nil {
						args = object{}
					}
					blocks = append(blocks, object{"toolUse": object{"toolUseId": toolID(b.ID), "name": b.Name, "input": args}})
				case "thinking":
					if m.API == ai.APIBedrockConverseStream && m.Model == model.ID && m.Provider == model.Provider {
						if b.Redacted {
							blocks = append(blocks, object{"reasoningContent": object{"redactedContent": b.ThinkingSignature}})
						} else if b.ThinkingSignature != "" {
							blocks = append(blocks, object{"reasoningContent": object{"reasoningText": object{"text": b.Thinking, "signature": b.ThinkingSignature}}})
						}
					}
				}
			}
		case "toolResult":
			role = "user"
			var result []object
			result, err = contentBlocks(m.ToolContent)
			if len(result) == 0 {
				result = []object{{"text": "(empty tool result)"}}
			}
			status := "success"
			if m.IsError {
				status = "error"
			}
			blocks = []object{{"toolResult": object{"toolUseId": toolID(m.ToolCallID), "content": result, "status": status}}}
		default:
			return nil, fmt.Errorf("unsupported Bedrock message role %q", role)
		}
		if err != nil {
			return nil, err
		}
		if len(blocks) == 0 {
			continue
		}
		if n := len(messages); n > 0 && messages[n-1].Role == role {
			messages[n-1].Content = append(messages[n-1].Content, blocks...)
		} else {
			messages = append(messages, message{Role: role, Content: blocks})
		}
	}
	maxTokens := model.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if opts.MaxTokens != nil && *opts.MaxTokens > 0 {
		maxTokens = *opts.MaxTokens
	}
	inference := object{"maxTokens": maxTokens}
	if opts.Temperature != nil {
		inference["temperature"] = *opts.Temperature
	}
	additional := object{}
	for _, params := range []map[string]any{model.SamplingParams, opts.SamplingParams} {
		for k, v := range params {
			switch k {
			case "temperature", "topP", "stopSequences":
				inference[k] = v
			default:
				additional[k] = v
			}
		}
	}
	if model.Reasoning && strings.Contains(model.ID, "anthropic.") && opts.Reasoning != "" && opts.Reasoning != ai.ThinkingOff {
		budget := max(1024, ai.ThinkingBudgetFor(opts.Reasoning, opts.ThinkingBudgets))
		if adaptiveThinking(model.ID) {
			effort := "high"
			switch opts.Reasoning {
			case ai.ThinkingMinimal, ai.ThinkingLow:
				effort = "low"
			case ai.ThinkingMedium:
				effort = "medium"
			case ai.ThinkingMax:
				if strings.Contains(model.ID, "opus-4-6") || strings.Contains(model.ID, "opus-4-7") || strings.Contains(model.ID, "sonnet-4-6") || strings.Contains(model.ID, "sonnet-5") || strings.Contains(model.ID, "opus-5") || strings.Contains(model.ID, "claude-fable") || strings.Contains(model.ID, "claude-mythos") {
					effort = "max"
				}
			case ai.ThinkingXHigh:
				if strings.Contains(model.ID, "opus-4-6") || strings.Contains(model.ID, "opus-4-7") || strings.Contains(model.ID, "opus-5") || strings.Contains(model.ID, "claude-fable") || strings.Contains(model.ID, "claude-mythos") {
					effort = "xhigh"
				}
			}
			additional["thinking"] = object{"type": "adaptive"}
			additional["output_config"] = object{"effort": effort}
		} else {
			additional["thinking"] = object{"type": "enabled", "budget_tokens": budget}
			inference["maxTokens"] = maxTokens + budget
		}
		delete(inference, "temperature")
		delete(inference, "topP")
	} else if model.Reasoning && strings.Contains(model.ID, "anthropic.") && opts.Reasoning == ai.ThinkingOff {
		additional["thinking"] = object{"type": "disabled"}
	}
	req := object{"messages": messages, "inferenceConfig": inference}
	if len(additional) > 0 {
		req["additionalModelRequestFields"] = additional
	}
	if ctx.SystemPrompt != "" {
		req["system"] = []object{{"text": ctx.SystemPrompt}}
	}
	if len(ctx.Tools) > 0 {
		tools := []object{}
		for _, t := range ctx.Tools {
			schema := t.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			if !json.Valid(schema) {
				return nil, fmt.Errorf("invalid schema for tool %q", t.Name)
			}
			spec := object{"name": t.Name, "inputSchema": object{"json": schema}}
			if t.Description != "" {
				spec["description"] = t.Description
			}
			tools = append(tools, object{"toolSpec": spec})
		}
		req["toolConfig"] = object{"tools": tools}
	}
	return req, nil
}
