// Package cli parses command line arguments for the maiku coding agent and
// renders --help / --version output.
package cli

import (
	"fmt"
	"strings"

	"github.com/mikus/maiku/codingagent"
)

// Mode is the output protocol used by print mode.
type Mode string

const (
	ModeText Mode = "text"
	ModeJSON Mode = "json"
	ModeRPC  Mode = "rpc"
)

// ValidThinkingLevels lists the accepted --thinking values.
var ValidThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// IsValidThinkingLevel reports whether level is an accepted --thinking value.
func IsValidThinkingLevel(level string) bool {
	for _, l := range ValidThinkingLevels {
		if l == level {
			return true
		}
	}
	return false
}

// Diagnostic is a parse warning or error surfaced to the user before the
// agent starts.
type Diagnostic struct {
	// Type is "warning" or "error".
	Type    string
	Message string
}

// Args is the parsed command line.
type Args struct {
	Provider           string
	Model              string
	APIKey             string
	SystemPrompt       string
	AppendSystemPrompt []string
	Thinking           string

	Continue  bool
	Session   string
	NoSession bool

	Help    bool
	Version bool

	Mode    Mode
	ModeSet bool
	Print   bool

	Tools        []string
	ExcludeTools []string
	NoTools      bool

	SkillPaths     []string
	NoSkills       bool
	NoContextFiles bool

	ListModels    string
	ListModelsSet bool
	Messages      []string
	FileArgs      []string
	UnknownFlags  map[string]string
	Diagnostics   []Diagnostic
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseArgs parses CLI arguments (excluding the program name). Unrecognized
// flags are collected rather than treated as fatal, matching the TS parser.
func ParseArgs(args []string) Args {
	result := Args{UnknownFlags: map[string]string{}}

	errorf := func(format string, a ...any) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Type: "error", Message: fmt.Sprintf(format, a...)})
	}
	warnf := func(format string, a ...any) {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Type: "warning", Message: fmt.Sprintf(format, a...)})
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() (string, bool) {
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}

		switch {
		case arg == "--help" || arg == "-h":
			result.Help = true

		case arg == "--version" || arg == "-v":
			result.Version = true

		case arg == "--mode":
			value, ok := next()
			if !ok {
				errorf("--mode requires a value (text, json)")
				break
			}
			switch Mode(value) {
			case ModeText, ModeJSON:
				result.Mode = Mode(value)
				result.ModeSet = true
			case ModeRPC:
				errorf("RPC mode is not ported yet; use --mode text or --mode json")
			default:
				errorf("Invalid mode %q. Valid values: text, json", value)
			}

		case arg == "--continue" || arg == "-c":
			result.Continue = true

		case arg == "--provider":
			if value, ok := next(); ok {
				result.Provider = value
			} else {
				errorf("--provider requires a value")
			}

		case arg == "--model":
			if value, ok := next(); ok {
				result.Model = value
			} else {
				errorf("--model requires a value")
			}

		case arg == "--api-key":
			if value, ok := next(); ok {
				result.APIKey = value
			} else {
				errorf("--api-key requires a value")
			}

		case arg == "--system-prompt":
			if value, ok := next(); ok {
				result.SystemPrompt = value
			} else {
				errorf("--system-prompt requires a value")
			}

		case arg == "--append-system-prompt":
			if value, ok := next(); ok {
				result.AppendSystemPrompt = append(result.AppendSystemPrompt, value)
			} else {
				errorf("--append-system-prompt requires a value")
			}

		case arg == "--thinking":
			value, ok := next()
			if !ok {
				errorf("--thinking requires a value")
				break
			}
			if IsValidThinkingLevel(value) {
				result.Thinking = value
			} else {
				warnf("Invalid thinking level %q. Valid values: %s", value, strings.Join(ValidThinkingLevels, ", "))
			}

		case arg == "--session":
			if value, ok := next(); ok {
				result.Session = value
			} else {
				errorf("--session requires a value")
			}

		case arg == "--no-session":
			result.NoSession = true

		case arg == "--no-tools" || arg == "-nt":
			result.NoTools = true

		case arg == "--tools" || arg == "-t":
			if value, ok := next(); ok {
				result.Tools = splitList(value)
			} else {
				errorf("--tools requires a value")
			}

		case arg == "--exclude-tools" || arg == "-xt":
			if value, ok := next(); ok {
				result.ExcludeTools = splitList(value)
			} else {
				errorf("--exclude-tools requires a value")
			}

		case arg == "--skill":
			if value, ok := next(); ok {
				result.SkillPaths = append(result.SkillPaths, value)
			} else {
				errorf("--skill requires a value")
			}

		case arg == "--no-skills":
			result.NoSkills = true

		case arg == "--no-context-files" || arg == "-nc":
			result.NoContextFiles = true

		case arg == "--print" || arg == "-p":
			result.Print = true
			// `-p "prompt"` takes the following argument as the message.
			if i+1 < len(args) {
				candidate := args[i+1]
				if !strings.HasPrefix(candidate, "@") && (!strings.HasPrefix(candidate, "-") || strings.HasPrefix(candidate, "---")) {
					result.Messages = append(result.Messages, candidate)
					i++
				}
			}

		case arg == "--list-models":
			result.ListModelsSet = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.HasPrefix(args[i+1], "@") {
				i++
				result.ListModels = args[i]
			}

		case strings.HasPrefix(arg, "@"):
			result.FileArgs = append(result.FileArgs, strings.TrimPrefix(arg, "@"))

		case strings.HasPrefix(arg, "--"):
			if eq := strings.Index(arg, "="); eq != -1 {
				result.UnknownFlags[arg[2:eq]] = arg[eq+1:]
				break
			}
			name := arg[2:]
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.HasPrefix(args[i+1], "@") {
				i++
				result.UnknownFlags[name] = args[i]
			} else {
				result.UnknownFlags[name] = "true"
			}

		case strings.HasPrefix(arg, "-"):
			errorf("Unknown option: %s", arg)

		default:
			result.Messages = append(result.Messages, arg)
		}
	}

	return result
}

// PrintVersion writes the version line to stdout.
func PrintVersion() {
	fmt.Printf("%s %s\n", codingagent.APP_NAME, codingagent.VERSION)
}

func PrintHelp() {
	fmt.Printf(`%s - lightweight coding agent (read, bash, edit, write)

Usage:
  %s [options] [@files...] [messages...]

Options:
  --print, -p [message]          Non-interactive mode: process prompts and exit
  --mode <mode>                  Output mode: text (default) or json
  --provider <name>              Provider id (see --list-models)
  --model <pattern>              Model id, or "provider/id"
  --api-key <key>                API key (defaults to provider env vars / ~/.maiku/agent/auth.json)
  --system-prompt <text>         Replace the default system prompt
  --append-system-prompt <text>  Append text to the system prompt (repeatable)
  --thinking <level>             Thinking level: %s
  --tools, -t <names>            Comma-separated allowlist of tool names
  --exclude-tools, -xt <names>   Comma-separated denylist of tool names
  --no-tools, -nt                Disable all tools
  --skill <path>                 Load a skill file or directory (repeatable)
  --no-skills                    Disable skill discovery and loading
  --no-context-files, -nc        Disable AGENTS.md and CLAUDE.md discovery
  --continue, -c                 Continue the most recent session for this directory
  --session <path|id>            Continue a specific session file or id prefix
  --no-session                   Don't persist a session (ephemeral)
  --list-models [search]         List known models and exit
  --help, -h                     Show this help
  --version, -v                  Show version number

Examples:
  %s -p "List the Go files in ./ai"
  %s --mode json -p "Summarize main.go"
  %s @notes.md -p "Turn these notes into a checklist"
  %s --model anthropic/claude-sonnet-4-5 -p "Explain this repo"
  %s --continue -p "What did we just change?"

Config:
  ~/%s/agent/settings.json   Defaults (provider, model, thinking, …)
  ~/%s/agent/auth.json       Stored API keys
  ~/%s/agent/sessions/       Session JSONL files
  %s                         Override agent dir
  %s                         Override session dir

Common API key env vars:
  ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, OPENROUTER_API_KEY,
  GROQ_API_KEY, DEEPSEEK_API_KEY, MISTRAL_API_KEY, XAI_API_KEY, …

Built-in tools: read, bash, edit, write (optional: grep, find, ls)
`,
		codingagent.APP_NAME,
		codingagent.APP_NAME,
		strings.Join(ValidThinkingLevels, ", "),
		codingagent.APP_NAME,
		codingagent.APP_NAME,
		codingagent.APP_NAME,
		codingagent.APP_NAME,
		codingagent.APP_NAME,
		codingagent.CONFIG_DIR_NAME,
		codingagent.CONFIG_DIR_NAME,
		codingagent.CONFIG_DIR_NAME,
		codingagent.ENV_AGENT_DIR,
		codingagent.ENV_SESSION_DIR,
	)
}
