package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

const findDefaultLimit = 1000

var findSchema = []byte(`{
	"type": "object",
	"properties": {
		"pattern": {"type": "string", "description": "Glob pattern to match files, e.g. '*.ts', '**/*.json', or 'src/**/*.spec.ts'"},
		"path": {"type": "string", "description": "Directory to search in (default: current directory)"},
		"limit": {"type": "number", "description": "Maximum number of results (default: 1000)"}
	},
	"required": ["pattern"]
}`)

// FindToolDetails mirrors TS FindToolDetails.
type FindToolDetails struct {
	Truncation         *TruncationResult
	ResultLimitReached int
}

// relativizeFindResultPath relativizes a find result against the search
// root and normalizes it to posix separators, preserving a trailing
// separator if present.
func relativizeFindResultPath(resultPath, searchPath string) string {
	hadTrailingSeparator := strings.HasSuffix(resultPath, string(os.PathSeparator)) || strings.HasSuffix(resultPath, "/")
	relativePath := resultPath
	if filepath.IsAbs(resultPath) {
		if rel, err := filepath.Rel(searchPath, resultPath); err == nil {
			relativePath = rel
		}
	}
	posixPath := filepath.ToSlash(relativePath)
	if hadTrailingSeparator && !strings.HasSuffix(posixPath, "/") {
		posixPath += "/"
	}
	return posixPath
}

func isInsideGitRepo(dir string) bool {
	current := dir
	for {
		if PathExists(filepath.Join(current, ".git")) {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

func effectiveGlobPattern(pattern string) string {
	if strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") && pattern != "**" {
		return "**/" + pattern
	}
	return pattern
}

// CreateFindTool ports coding-agent's find tool (core/tools/find.ts),
// EXECUTE logic only (TUI rendering omitted).
//
// Deviation from TS: when the `fd` binary is unavailable, the fallback
// walker skips only ".git" and "node_modules" directories; it does not
// parse .gitignore files (fd's native --no-require-git/git-aware behavior
// is not replicated).
func CreateFindTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name: "find",
			Description: fmt.Sprintf(
				"Search for files by glob pattern. Returns matching file paths relative to the search directory. Respects .gitignore. Output is truncated to %d results or %dKB (whichever is hit first).",
				findDefaultLimit, DefaultMaxBytes/1024,
			),
			Parameters: findSchema,
		},
		Label: "find",
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			pattern, _ := argString(params, "pattern")
			searchDir := argStringOr(params, "path", ".")
			limitPtr := argIntPtr(params, "limit")

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			searchPath := ResolveToCwd(searchDir, cwd)
			effectiveLimit := findDefaultLimit
			if limitPtr != nil {
				effectiveLimit = *limitPtr
			}

			if !PathExists(searchPath) {
				return agent.AgentToolResult{}, fmt.Errorf("path not found: %s", searchPath)
			}

			var relativized []string
			var err error
			if fdPath, lookErr := exec.LookPath("fd"); lookErr == nil {
				relativized, err = findWithFd(ctx, fdPath, pattern, searchPath, effectiveLimit)
			} else {
				relativized, err = findWithWalk(pattern, searchPath, effectiveLimit)
			}
			if err != nil {
				return agent.AgentToolResult{}, err
			}

			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}

			if len(relativized) == 0 {
				return agent.AgentToolResult{
					Content: []ai.ToolResultContent{{Type: "text", Text: "No files found matching pattern"}},
				}, nil
			}

			resultLimitReached := len(relativized) >= effectiveLimit
			rawOutput := strings.Join(relativized, "\n")
			truncation := TruncateHead(rawOutput, &TruncationOptions{MaxLines: 1 << 30})
			resultOutput := truncation.Content

			details := &FindToolDetails{}
			hasDetails := false
			var notices []string
			if resultLimitReached {
				notices = append(notices, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
				details.ResultLimitReached = effectiveLimit
				hasDetails = true
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
				hasDetails = true
			}
			if len(notices) > 0 {
				resultOutput += fmt.Sprintf("\n\n[%s]", strings.Join(notices, ". "))
			}

			result := agent.AgentToolResult{
				Content: []ai.ToolResultContent{{Type: "text", Text: resultOutput}},
			}
			if hasDetails {
				result.Details = details
			}
			return result, nil
		},
	}
}

func findWithFd(ctx context.Context, fdPath, pattern, searchPath string, effectiveLimit int) ([]string, error) {
	args := []string{"--glob", "--color=never", "--hidden"}
	if !isInsideGitRepo(searchPath) {
		args = append(args, "--no-require-git")
	}
	args = append(args, "--max-results", fmt.Sprintf("%d", effectiveLimit))

	effectivePattern := pattern
	if strings.Contains(pattern, "/") {
		args = append(args, "--full-path")
		effectivePattern = effectiveGlobPattern(pattern)
	}
	args = append(args, "--", effectivePattern, searchPath)

	cmd := exec.CommandContext(ctx, fdPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to run fd: %w", err)
	}
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to run fd: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line != "" {
			lines = append(lines, line)
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, errOperationAborted
	}
	if waitErr != nil {
		if len(lines) == 0 {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("fd exited with code %v", waitErr)
			}
			return nil, errors.New(errMsg)
		}
	}

	relativized := make([]string, 0, len(lines))
	for _, line := range lines {
		relativized = append(relativized, relativizeFindResultPath(line, searchPath))
	}
	return relativized, nil
}

func findWithWalk(pattern, searchPath string, effectiveLimit int) ([]string, error) {
	effectivePattern := effectiveGlobPattern(pattern)
	hasSlash := strings.Contains(pattern, "/")

	var results []string
	err := filepath.WalkDir(searchPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == searchPath {
			return nil
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules") {
			return filepath.SkipDir
		}
		if len(results) >= effectiveLimit {
			return filepath.SkipAll
		}

		rel, relErr := filepath.Rel(searchPath, p)
		if relErr != nil {
			return relErr
		}
		relPosix := filepath.ToSlash(rel)
		candidate := name
		if hasSlash {
			candidate = relPosix
		}

		matched, _ := doublestar.Match(effectivePattern, candidate)
		if matched {
			out := relPosix
			if d.IsDir() {
				out += "/"
			}
			results = append(results, out)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
