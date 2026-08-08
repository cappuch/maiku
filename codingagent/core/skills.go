package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mikus/maiku/codingagent"
)

// Limits from the Agent Skills spec.
const (
	MaxSkillNameLength        = 64
	MaxSkillDescriptionLength = 1024
)

// ResourceDiagnostic is a non-fatal problem found while loading a resource.
type ResourceDiagnostic struct {
	// Type is "warning" or "collision".
	Type    string
	Message string
	Path    string
}

// Skill is a SKILL.md discovered on disk.
type Skill struct {
	Name        string
	Description string
	// FilePath is the absolute path of the skill markdown file.
	FilePath string
	// BaseDir is the directory relative paths inside the skill resolve against.
	BaseDir string
	// Source is "user", "project", or "path".
	Source string
	// DisableModelInvocation hides the skill from the system prompt; it can
	// then only be invoked explicitly.
	DisableModelInvocation bool
}

// SkillsResult is the outcome of a skill discovery pass.
type SkillsResult struct {
	Skills      []Skill
	Diagnostics []ResourceDiagnostic
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// ParseFrontmatter splits leading YAML frontmatter from a markdown document.
// It returns the raw YAML (without the delimiters) and the remaining body.
func ParseFrontmatter(content string) (frontmatter string, body string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return "", normalized
	}
	end := strings.Index(normalized[3:], "\n---")
	if end == -1 {
		return "", normalized
	}
	end += 3
	return normalized[4:end], strings.TrimSpace(normalized[end+4:])
}

func validateSkillName(name string) []string {
	var errs []string
	if len(name) > MaxSkillNameLength {
		errs = append(errs, fmt.Sprintf("name exceeds %d characters (%d)", MaxSkillNameLength, len(name)))
	}
	if !skillNameRe.MatchString(name) {
		errs = append(errs, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errs = append(errs, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errs = append(errs, "name must not contain consecutive hyphens")
	}
	return errs
}

func validateSkillDescription(description string) []string {
	if strings.TrimSpace(description) == "" {
		return []string{"description is required"}
	}
	if len(description) > MaxSkillDescriptionLength {
		return []string{fmt.Sprintf("description exceeds %d characters (%d)", MaxSkillDescriptionLength, len(description))}
	}
	return nil
}

// LoadSkillFromFile parses one skill markdown file. A skill without a
// description is rejected; other spec violations are reported as warnings but
// still load.
func LoadSkillFromFile(path, source string) (Skill, []ResourceDiagnostic, bool) {
	var diagnostics []ResourceDiagnostic
	warn := func(message string) {
		diagnostics = append(diagnostics, ResourceDiagnostic{Type: "warning", Message: message, Path: path})
	}

	content, err := os.ReadFile(path)
	if err != nil {
		warn(err.Error())
		return Skill{}, diagnostics, false
	}

	raw, _ := ParseFrontmatter(string(content))
	var frontmatter skillFrontmatter
	if raw != "" {
		if err := yaml.Unmarshal([]byte(raw), &frontmatter); err != nil {
			warn(fmt.Sprintf("failed to parse skill frontmatter: %v", err))
			return Skill{}, diagnostics, false
		}
	}

	skillDir := filepath.Dir(path)
	for _, message := range validateSkillDescription(frontmatter.Description) {
		warn(message)
	}

	name := frontmatter.Name
	if name == "" {
		name = filepath.Base(skillDir)
	}
	for _, message := range validateSkillName(name) {
		warn(message)
	}

	if strings.TrimSpace(frontmatter.Description) == "" {
		return Skill{}, diagnostics, false
	}

	return Skill{
		Name:                   name,
		Description:            frontmatter.Description,
		FilePath:               path,
		BaseDir:                skillDir,
		Source:                 source,
		DisableModelInvocation: frontmatter.DisableModelInvocation,
	}, diagnostics, true
}

// LoadSkillsFromDir scans a directory for skills.
//
// Discovery rules match the TypeScript loader: a directory containing
// SKILL.md is a skill root and is not descended into; otherwise direct .md
// children of the scan root are loaded and subdirectories are searched for
// SKILL.md.
func LoadSkillsFromDir(dir, source string) SkillsResult {
	return loadSkillsFromDir(dir, source, true)
}

func loadSkillsFromDir(dir, source string, includeRootFiles bool) SkillsResult {
	var result SkillsResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if entry.Name() != "SKILL.md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		skill, diagnostics, ok := LoadSkillFromFile(path, source)
		if ok {
			result.Skills = append(result.Skills, skill)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		return result
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}

		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		if info.IsDir() {
			sub := loadSkillsFromDir(path, source, false)
			result.Skills = append(result.Skills, sub.Skills...)
			result.Diagnostics = append(result.Diagnostics, sub.Diagnostics...)
			continue
		}

		if !includeRootFiles || !info.Mode().IsRegular() || !strings.HasSuffix(name, ".md") {
			continue
		}
		skill, diagnostics, ok := LoadSkillFromFile(path, source)
		if ok {
			result.Skills = append(result.Skills, skill)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}

	return result
}

// LoadSkillsOptions configures LoadSkills.
type LoadSkillsOptions struct {
	// Cwd is the project root used to find .maiku/skills.
	Cwd string
	// AgentDir is the user config dir used to find skills/. Defaults to
	// the standard agent directory.
	AgentDir string
	// SkillPaths are extra files or directories to scan.
	SkillPaths []string
	// IncludeDefaults enables the user and project skill directories.
	IncludeDefaults bool
}

// UserSkillsDir returns the user-level skills directory.
func UserSkillsDir(agentDir string) string { return filepath.Join(agentDir, "skills") }

// ProjectSkillsDir returns the project-level skills directory.
func ProjectSkillsDir(cwd string) string {
	return filepath.Join(cwd, codingagent.CONFIG_DIR_NAME, "skills")
}

// LoadSkills discovers skills from the user directory, the project
// directory, and any explicit paths. The first skill claiming a name wins;
// later duplicates are reported as collisions.
func LoadSkills(options LoadSkillsOptions) SkillsResult {
	agentDir := options.AgentDir
	if agentDir == "" {
		agentDir = codingagent.GetAgentDir()
	}
	cwd := codingagent.ExpandTildePath(options.Cwd)
	agentDir = codingagent.ExpandTildePath(agentDir)

	userDir := UserSkillsDir(agentDir)
	projectDir := ProjectSkillsDir(cwd)

	var (
		ordered     []Skill
		byName      = map[string]Skill{}
		seenPaths   = map[string]bool{}
		diagnostics []ResourceDiagnostic
		collisions  []ResourceDiagnostic
	)

	add := func(result SkillsResult) {
		diagnostics = append(diagnostics, result.Diagnostics...)
		for _, skill := range result.Skills {
			realPath := skill.FilePath
			if resolved, err := filepath.EvalSymlinks(skill.FilePath); err == nil {
				realPath = resolved
			}
			if seenPaths[realPath] {
				continue
			}
			if existing, exists := byName[skill.Name]; exists {
				collisions = append(collisions, ResourceDiagnostic{
					Type:    "collision",
					Message: fmt.Sprintf("name %q collision (kept %s)", skill.Name, existing.FilePath),
					Path:    skill.FilePath,
				})
				continue
			}
			byName[skill.Name] = skill
			seenPaths[realPath] = true
			ordered = append(ordered, skill)
		}
	}

	if options.IncludeDefaults {
		add(loadSkillsFromDir(userDir, "user", true))
		add(loadSkillsFromDir(projectDir, "project", true))
	}

	for _, rawPath := range options.SkillPaths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		path = codingagent.ExpandTildePath(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}

		info, err := os.Stat(path)
		if err != nil {
			diagnostics = append(diagnostics, ResourceDiagnostic{
				Type: "warning", Message: "skill path does not exist", Path: path,
			})
			continue
		}

		source := "path"
		if !options.IncludeDefaults {
			switch {
			case isUnderPath(path, userDir):
				source = "user"
			case isUnderPath(path, projectDir):
				source = "project"
			}
		}

		switch {
		case info.IsDir():
			add(loadSkillsFromDir(path, source, true))
		case info.Mode().IsRegular() && strings.HasSuffix(path, ".md"):
			skill, fileDiagnostics, ok := LoadSkillFromFile(path, source)
			if ok {
				add(SkillsResult{Skills: []Skill{skill}, Diagnostics: fileDiagnostics})
			} else {
				diagnostics = append(diagnostics, fileDiagnostics...)
			}
		default:
			diagnostics = append(diagnostics, ResourceDiagnostic{
				Type: "warning", Message: "skill path is not a markdown file", Path: path,
			})
		}
	}

	return SkillsResult{Skills: ordered, Diagnostics: append(diagnostics, collisions...)}
}

func isUnderPath(target, root string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(filepath.Separator))
}

// FormatSkillsForPrompt renders the available-skills section of the system
// prompt in the XML shape defined by the Agent Skills standard. Skills with
// DisableModelInvocation are omitted.
func FormatSkillsForPrompt(skills []Skill) string {
	var visible []Skill
	for _, skill := range skills {
		if !skill.DisableModelInvocation {
			visible = append(visible, skill)
		}
	}
	if len(visible) == 0 {
		return ""
	}

	lines := []string{
		"\n\nThe following skills provide specialized instructions for specific tasks.",
		"Use the read tool to load a skill's file when the task matches its description.",
		"When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	}
	for _, skill := range visible {
		lines = append(lines,
			"  <skill>",
			"    <name>"+escapeXML(skill.Name)+"</name>",
			"    <description>"+escapeXML(skill.Description)+"</description>",
			"    <location>"+escapeXML(skill.FilePath)+"</location>",
			"  </skill>",
		)
	}
	lines = append(lines, "</available_skills>")
	return strings.Join(lines, "\n")
}

func escapeXML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
