// Package core wires the coding agent's runtime: system prompt, session
// persistence, model resolution, and the agent session wrapper.
package core

import (
	"fmt"
	"strings"
)

// DefaultToolNames are the built-in tools enabled when the caller does not
// pass an allowlist.
var DefaultToolNames = []string{"read", "bash", "edit", "write", "miru", "web_search", "curl"}

// DefaultToolSnippets are the one-line tool descriptions rendered into the
// system prompt's "Available tools" section.
var DefaultToolSnippets = map[string]string{
	"read":       "Read file contents (with optional line offset/limit)",
	"bash":       "Execute shell commands in the working directory",
	"edit":       "Edit files with exact find/replace",
	"write":      "Write files (creates or overwrites)",
	"grep":       "Search file contents",
	"find":       "Find files by glob pattern",
	"ls":         "List directory contents",
	"miru":       "Search repository code by meaning",
	"web_search": "Search the web with DuckDuckGo HTML search",
	"curl":       "Fetch HTTP(S) page content with a browser user agent",
	"subagent":   "Delegate a self-contained task to an independent child Maiku and receive a Markdown report",
}

// BuildSystemPromptOptions configures BuildSystemPrompt.
type BuildSystemPromptOptions struct {
	// CustomPrompt replaces the default prompt body when non-empty.
	CustomPrompt string
	// SelectedTools are the tool names available this run. Defaults to
	// DefaultToolNames.
	SelectedTools []string
	// ToolSnippets are one-line descriptions keyed by tool name. Only tools
	// with a snippet appear in the prompt's tool list.
	ToolSnippets map[string]string
	// PromptGuidelines are extra guideline bullets inserted before the
	// always-on ones.
	PromptGuidelines []string
	// AppendSystemPrompt is appended after the prompt body.
	AppendSystemPrompt string
	// Cwd is the agent's working directory.
	Cwd string
	// ContextFiles are project instruction files (AGENTS.md and friends).
	ContextFiles []ContextFile
	// Skills are the discovered skills advertised to the model. They are
	// only rendered when the read tool is available.
	Skills []Skill
}

// BuildSystemPrompt renders the system prompt for a run.
func BuildSystemPrompt(options BuildSystemPromptOptions) string {
	cwd := strings.ReplaceAll(options.Cwd, "\\", "/")

	appendSection := ""
	if options.AppendSystemPrompt != "" {
		appendSection = "\n\n" + options.AppendSystemPrompt
	}

	tools := options.SelectedTools
	if tools == nil {
		tools = DefaultToolNames
	}
	has := map[string]bool{}
	for _, name := range tools {
		has[name] = true
	}

	if options.CustomPrompt != "" {
		prompt := options.CustomPrompt + appendSection
		prompt += formatContextFiles(options.ContextFiles)
		// A custom prompt with no explicit tool list still gets the default
		// set, which includes read.
		if options.SelectedTools == nil || has["read"] {
			prompt += FormatSkillsForPrompt(options.Skills)
		}
		return prompt + fmt.Sprintf("\nCurrent working directory: %s", cwd)
	}

	snippets := options.ToolSnippets
	if snippets == nil {
		snippets = DefaultToolSnippets
	}

	var toolLines []string
	for _, name := range tools {
		if snippet, ok := snippets[name]; ok {
			toolLines = append(toolLines, fmt.Sprintf("- %s: %s", name, snippet))
		}
	}
	toolsList := "(none)"
	if len(toolLines) > 0 {
		toolsList = strings.Join(toolLines, "\n")
	}

	var guidelines []string
	seen := map[string]bool{}
	addGuideline := func(guideline string) {
		guideline = strings.TrimSpace(guideline)
		if guideline == "" || seen[guideline] {
			return
		}
		seen[guideline] = true
		guidelines = append(guidelines, guideline)
	}

	if has["bash"] && !has["grep"] && !has["find"] && !has["ls"] {
		addGuideline("Use the bash tool for shell-based file operations and searches")
	}
	if has["read"] && (has["edit"] || has["write"]) {
		addGuideline("Prefer reading a file before editing it")
	}
	if has[SubagentToolName] {
		addGuideline("Use subagents for self-contained delegated work. Issue multiple subagent calls in one response when tasks are independent, then review their reports and remain responsible for the overall result")
	}
	for _, guideline := range options.PromptGuidelines {
		addGuideline(guideline)
	}
	addGuideline("Be concise in your responses")
	addGuideline("Show file paths clearly when working with files")
	addGuideline("Act autonomously: use tools immediately to fulfill the user's request. Do not ask for permission before running commands or editing files. Do not present menus of options — just do the work and report results.")

	guidelinesList := ""
	for _, guideline := range guidelines {
		guidelinesList += "- " + guideline + "\n"
	}
	guidelinesList = strings.TrimRight(guidelinesList, "\n")

	prompt := fmt.Sprintf(`You are an expert coding assistant operating inside maiku, a lightweight coding agent. You help users by reading files, executing commands, editing code, and writing new files.

You have real tools — call them. Never pretend to act; always use tools to inspect and change the workspace. When the user asks you to build something, create/edit the files and run what you need without waiting for confirmation.

Available tools:
%s

Guidelines:
%s`, toolsList, guidelinesList)

	prompt += appendSection
	prompt += formatContextFiles(options.ContextFiles)
	if has["read"] {
		prompt += FormatSkillsForPrompt(options.Skills)
	}

	return prompt + fmt.Sprintf("\nCurrent working directory: %s", cwd)
}

func formatContextFiles(contextFiles []ContextFile) string {
	if len(contextFiles) == 0 {
		return ""
	}
	var section strings.Builder
	section.WriteString("\n\n<project_context>\n\n")
	section.WriteString("Project-specific instructions and guidelines:\n\n")
	for _, file := range contextFiles {
		fmt.Fprintf(&section, "<project_instructions path=%q>\n%s\n</project_instructions>\n\n", file.Path, file.Content)
	}
	section.WriteString("</project_context>\n")
	return section.String()
}
