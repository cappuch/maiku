package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const grepDefaultLimit = 100

var grepSchema = []byte(`{
	"type": "object",
	"properties": {
		"pattern": {"type": "string", "description": "Search pattern (regex or literal string)"},
		"path": {"type": "string", "description": "Directory or file to search (default: current directory)"},
		"glob": {"type": "string", "description": "Filter files by glob pattern, e.g. '*.ts' or '**/*.spec.ts'"},
		"ignoreCase": {"type": "boolean", "description": "Case-insensitive search (default: false)"},
		"literal": {"type": "boolean", "description": "Treat pattern as literal string instead of regex (default: false)"},
		"context": {"type": "number", "description": "Number of lines to show before and after each match (default: 0)"},
		"limit": {"type": "number", "description": "Maximum number of matches to return (default: 100)"}
	},
	"required": ["pattern"]
}`)

// GrepToolDetails mirrors TS GrepToolDetails.
type GrepToolDetails struct {
	Truncation        *TruncationResult
	MatchLimitReached int
	LinesTruncated    bool
}

type grepMatch struct {
	filePath   string
	lineNumber int
	lineText   string
	hasText    bool
}

// CreateGrepTool ports coding-agent's grep tool (core/tools/grep.ts),
// EXECUTE logic only (TUI rendering omitted).
//
// Deviation from TS: when `rg` is unavailable, the fallback walker skips
// only ".git" and "node_modules" directories (no .gitignore parsing) and
// uses a simple heuristic (a NUL byte in the first 8KB) to skip binary
// files instead of ripgrep's binary-content detection.
func CreateGrepTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: "grep",
			Description: fmt.Sprintf(
				"Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output is truncated to %d matches or %dKB (whichever is hit first). Long lines are truncated to %d chars.",
				grepDefaultLimit, DefaultMaxBytes/1024, GrepMaxLineLength,
			),
			Parameters: grepSchema,
		},
		Label: "grep",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			pattern, _ := argString(params, "pattern")
			searchDir := argStringOr(params, "path", ".")
			globPattern, _ := argString(params, "glob")
			ignoreCase, _ := argBool(params, "ignoreCase")
			literal, _ := argBool(params, "literal")
			contextPtr := argIntPtr(params, "context")
			limitPtr := argIntPtr(params, "limit")

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			searchPath := ResolveToCwd(searchDir, cwd)
			info, statErr := os.Stat(searchPath)
			if statErr != nil {
				return agent.AgentToolResult{}, fmt.Errorf("path not found: %s", searchPath)
			}
			isDirectory := info.IsDir()

			contextValue := 0
			if contextPtr != nil && *contextPtr > 0 {
				contextValue = *contextPtr
			}
			effectiveLimit := grepDefaultLimit
			if limitPtr != nil && *limitPtr > 0 {
				effectiveLimit = *limitPtr
			} else if limitPtr != nil {
				effectiveLimit = 1
			}

			var matches []grepMatch
			var matchLimitReached bool
			var runErr error
			if rgPath, lookErr := exec.LookPath("rg"); lookErr == nil {
				matches, matchLimitReached, runErr = grepWithRipgrep(ctx, rgPath, pattern, searchPath, globPattern, ignoreCase, literal, effectiveLimit)
			} else {
				matches, matchLimitReached, runErr = grepWithWalk(pattern, searchPath, globPattern, ignoreCase, literal, effectiveLimit)
			}
			if runErr != nil {
				return agent.AgentToolResult{}, runErr
			}

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			if len(matches) == 0 {
				return agent.AgentToolResult{
					Content: []ai.ToolResultContent{{Type: "text", Text: "No matches found"}},
				}, nil
			}

			formatPath := func(filePath string) string {
				if isDirectory {
					if rel, err := filepath.Rel(searchPath, filePath); err == nil && !strings.HasPrefix(rel, "..") {
						return filepath.ToSlash(rel)
					}
				}
				return filepath.Base(filePath)
			}

			fileLineCache := map[string][]string{}
			getFileLines := func(filePath string) []string {
				if lines, ok := fileLineCache[filePath]; ok {
					return lines
				}
				data, err := os.ReadFile(filePath)
				var lines []string
				if err == nil {
					content := strings.ReplaceAll(string(data), "\r\n", "\n")
					content = strings.ReplaceAll(content, "\r", "\n")
					lines = strings.Split(content, "\n")
				}
				fileLineCache[filePath] = lines
				return lines
			}

			linesTruncated := false
			var outputLines []string

			formatBlock := func(filePath string, lineNumber int) []string {
				relativePath := formatPath(filePath)
				lines := getFileLines(filePath)
				if len(lines) == 0 {
					return []string{fmt.Sprintf("%s:%d: (unable to read file)", relativePath, lineNumber)}
				}
				var block []string
				start, end := lineNumber, lineNumber
				if contextValue > 0 {
					start = max(lineNumber-contextValue, 1)
					end = min(lineNumber+contextValue, len(lines))
				}
				for current := start; current <= end; current++ {
					lineText := ""
					if current-1 < len(lines) {
						lineText = lines[current-1]
					}
					sanitized := strings.ReplaceAll(lineText, "\r", "")
					isMatchLine := current == lineNumber
					truncatedText, wasTruncated := TruncateLine(sanitized, 0)
					if wasTruncated {
						linesTruncated = true
					}
					if isMatchLine {
						block = append(block, fmt.Sprintf("%s:%d: %s", relativePath, current, truncatedText))
					} else {
						block = append(block, fmt.Sprintf("%s-%d- %s", relativePath, current, truncatedText))
					}
				}
				return block
			}

			for _, m := range matches {
				if contextValue == 0 && m.hasText {
					relativePath := formatPath(m.filePath)
					sanitized := strings.ReplaceAll(m.lineText, "\r\n", "\n")
					sanitized = strings.ReplaceAll(sanitized, "\r", "")
					sanitized = strings.TrimSuffix(sanitized, "\n")
					truncatedText, wasTruncated := TruncateLine(sanitized, 0)
					if wasTruncated {
						linesTruncated = true
					}
					outputLines = append(outputLines, fmt.Sprintf("%s:%d: %s", relativePath, m.lineNumber, truncatedText))
				} else {
					outputLines = append(outputLines, formatBlock(m.filePath, m.lineNumber)...)
				}
			}

			rawOutput := strings.Join(outputLines, "\n")
			truncation := TruncateHead(rawOutput, &TruncationOptions{MaxLines: 1 << 30})
			output := truncation.Content

			details := &GrepToolDetails{}
			hasDetails := false
			var notices []string
			if matchLimitReached {
				notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
				details.MatchLimitReached = effectiveLimit
				hasDetails = true
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
				hasDetails = true
			}
			if linesTruncated {
				notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", GrepMaxLineLength))
				details.LinesTruncated = true
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

type rgJSONEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Lines      struct {
			Text string `json:"text"`
		} `json:"lines"`
	} `json:"data"`
}

func grepWithRipgrep(ctx context.Context, rgPath, pattern, searchPath, glob string, ignoreCase, literal bool, effectiveLimit int) ([]grepMatch, bool, error) {
	args := []string{"--json", "--line-number", "--color=never", "--hidden"}
	if ignoreCase {
		args = append(args, "--ignore-case")
	}
	if literal {
		args = append(args, "--fixed-strings")
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, "--", pattern, searchPath)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("failed to run ripgrep: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("failed to run ripgrep: %w", err)
	}

	var matches []grepMatch
	matchLimitReached := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		if len(matches) >= effectiveLimit {
			break
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event rgJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Type != "match" {
			continue
		}
		if event.Data.Path.Text == "" {
			continue
		}
		matches = append(matches, grepMatch{
			filePath:   event.Data.Path.Text,
			lineNumber: event.Data.LineNumber,
			lineText:   event.Data.Lines.Text,
			hasText:    true,
		})
		if len(matches) >= effectiveLimit {
			matchLimitReached = true
			break
		}
	}

	if matchLimitReached {
		_ = cmd.Process.Kill()
	}
	// Drain remaining stdout so Wait doesn't block on a full pipe buffer.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			if _, err := stdout.Read(buf); err != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, false, errOperationAborted
	}
	if !matchLimitReached && waitErr != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			code := exitErr.ExitCode()
			if code != 0 && code != 1 {
				errMsg := strings.TrimSpace(stderrBuf.String())
				if errMsg == "" {
					errMsg = fmt.Sprintf("ripgrep exited with code %d", code)
				}
				return nil, false, errors.New(errMsg)
			}
		} else {
			return nil, false, waitErr
		}
	}

	return matches, matchLimitReached, nil
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	if err := f.Close(); err != nil {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) != -1
}

func grepWithWalk(pattern, searchPath string, glob string, ignoreCase, literal bool, effectiveLimit int) ([]grepMatch, bool, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, false, err
	}

	var re *regexp.Regexp
	if !literal {
		p := pattern
		if ignoreCase {
			p = "(?i)" + p
		}
		re, err = regexp.Compile(p)
		if err != nil {
			return nil, false, err
		}
	}

	lineMatches := func(line string) bool {
		if literal {
			if ignoreCase {
				return strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
			}
			return strings.Contains(line, pattern)
		}
		return re.MatchString(line)
	}

	var matches []grepMatch
	matchLimitReached := false

	visit := func(filePath string) error {
		if len(matches) >= effectiveLimit {
			return filepath.SkipAll
		}
		if glob != "" {
			rel, relErr := filepath.Rel(searchPath, filePath)
			candidate := filepath.Base(filePath)
			if relErr == nil {
				candidate = filepath.ToSlash(rel)
			}
			if ok, _ := doublestar.Match(glob, candidate); !ok {
				return nil
			}
		}
		if isBinaryFile(filePath) {
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		content := strings.ReplaceAll(string(data), "\r\n", "\n")
		content = strings.ReplaceAll(content, "\r", "\n")
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if len(matches) >= effectiveLimit {
				matchLimitReached = true
				return filepath.SkipAll
			}
			if lineMatches(line) {
				matches = append(matches, grepMatch{filePath: filePath, lineNumber: i + 1, lineText: line, hasText: true})
			}
		}
		return nil
	}

	if !info.IsDir() {
		if err := visit(searchPath); err != nil && !errors.Is(err, filepath.SkipAll) {
			return nil, false, err
		}
		return matches, matchLimitReached, nil
	}

	walkErr := filepath.WalkDir(searchPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(p)
	})
	if walkErr != nil {
		return nil, false, walkErr
	}
	return matches, matchLimitReached, nil
}
