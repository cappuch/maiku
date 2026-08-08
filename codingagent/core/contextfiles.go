package core

import (
	"os"
	"path/filepath"

	"github.com/mikus/maiku/codingagent"
)

// ContextFileNames are the per-directory context files, in the priority order
// used by the TypeScript resource loader. Only the first match in a directory
// is loaded.
var ContextFileNames = []string{"AGENTS.override.md", "AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}

// ContextFile is a project instruction file included in the system prompt.
type ContextFile struct {
	Path    string
	Content string
}

// LoadContextFileFromDir returns the first context file present in dir.
func LoadContextFileFromDir(dir string) (ContextFile, bool) {
	for _, name := range ContextFileNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return ContextFile{Path: path, Content: string(content)}, true
	}
	return ContextFile{}, false
}

// LoadProjectContextFiles collects context files for a run: the agent
// directory's file first, then every ancestor of cwd ordered outermost-first
// so that closer files appear later and win. A path is included at most once.
func LoadProjectContextFiles(cwd, agentDir string) []ContextFile {
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	resolvedCwd := codingagent.ExpandTildePath(cwd)
	resolvedAgentDir := codingagent.ExpandTildePath(agentDir)

	var files []ContextFile
	seen := map[string]bool{}

	if global, ok := LoadContextFileFromDir(resolvedAgentDir); ok {
		files = append(files, global)
		seen[global.Path] = true
	}

	var ancestors []ContextFile
	for dir := resolvedCwd; ; {
		if file, ok := LoadContextFileFromDir(dir); ok && !seen[file.Path] {
			seen[file.Path] = true
			ancestors = append([]ContextFile{file}, ancestors...)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return append(files, ancestors...)
}
