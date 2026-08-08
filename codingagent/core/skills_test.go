package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatterSplitsYAMLAndBody(t *testing.T) {
	frontmatter, body := ParseFrontmatter("---\nname: demo\ndescription: A demo\n---\n\n# Heading\nBody text\n")
	if !strings.Contains(frontmatter, "name: demo") {
		t.Errorf("frontmatter = %q", frontmatter)
	}
	if !strings.HasPrefix(body, "# Heading") {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatterWithoutDelimiters(t *testing.T) {
	frontmatter, body := ParseFrontmatter("# Just markdown\n")
	if frontmatter != "" {
		t.Errorf("frontmatter = %q, want empty", frontmatter)
	}
	if body != "# Just markdown\n" {
		t.Errorf("body = %q", body)
	}
}

func TestLoadSkillFromFileReadsFrontmatter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-skill")
	path := filepath.Join(dir, "SKILL.md")
	writeFile(t, path, `---
name: my-skill
description: >-
  Does a thing across
  multiple lines
disable-model-invocation: true
---

Instructions here.
`)

	skill, diagnostics, ok := LoadSkillFromFile(path, "user")
	if !ok {
		t.Fatalf("skill should load, diagnostics: %v", diagnostics)
	}
	if skill.Name != "my-skill" {
		t.Errorf("name = %q", skill.Name)
	}
	if skill.Description != "Does a thing across multiple lines" {
		t.Errorf("description = %q", skill.Description)
	}
	if !skill.DisableModelInvocation {
		t.Error("disable-model-invocation should be true")
	}
	if skill.BaseDir != dir {
		t.Errorf("baseDir = %q, want %q", skill.BaseDir, dir)
	}
}

func TestLoadSkillFromFileFallsBackToDirectoryName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fallback-name", "SKILL.md")
	writeFile(t, path, "---\ndescription: No name given\n---\n\nBody\n")

	skill, _, ok := LoadSkillFromFile(path, "project")
	if !ok {
		t.Fatal("skill should load without an explicit name")
	}
	if skill.Name != "fallback-name" {
		t.Errorf("name = %q, want parent directory name", skill.Name)
	}
}

func TestLoadSkillFromFileRejectsMissingDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-desc", "SKILL.md")
	writeFile(t, path, "---\nname: no-desc\n---\n\nBody\n")

	_, diagnostics, ok := LoadSkillFromFile(path, "user")
	if ok {
		t.Fatal("a skill without a description should be rejected")
	}
	if len(diagnostics) == 0 {
		t.Error("want a diagnostic explaining the rejection")
	}
}

func TestLoadSkillFromFileWarnsOnInvalidName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weird", "SKILL.md")
	writeFile(t, path, "---\nname: Not Valid\ndescription: Loads anyway\n---\n")

	skill, diagnostics, ok := LoadSkillFromFile(path, "user")
	if !ok {
		t.Fatal("an invalid name is a warning, not a rejection")
	}
	if skill.Name != "Not Valid" {
		t.Errorf("name = %q", skill.Name)
	}
	if len(diagnostics) == 0 {
		t.Error("want a name validation warning")
	}
}

func TestLoadSkillsDiscoversUserAndProjectDirs(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()

	writeFile(t, filepath.Join(UserSkillsDir(agentDir), "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: User skill\n---\n")
	writeFile(t, filepath.Join(ProjectSkillsDir(cwd), "beta", "SKILL.md"),
		"---\nname: beta\ndescription: Project skill\n---\n")
	// A directory holding SKILL.md is a skill root and is not descended into.
	writeFile(t, filepath.Join(ProjectSkillsDir(cwd), "beta", "nested", "SKILL.md"),
		"---\nname: nested\ndescription: Should be ignored\n---\n")

	result := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true})

	names := map[string]bool{}
	for _, skill := range result.Skills {
		names[skill.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("want alpha and beta, got %v", names)
	}
	if names["nested"] {
		t.Error("a skill root should not be descended into")
	}
}

func TestLoadSkillsReportsNameCollision(t *testing.T) {
	cwd := t.TempDir()
	agentDir := t.TempDir()
	writeFile(t, filepath.Join(UserSkillsDir(agentDir), "dup", "SKILL.md"),
		"---\nname: dup\ndescription: User wins\n---\n")
	writeFile(t, filepath.Join(ProjectSkillsDir(cwd), "dup", "SKILL.md"),
		"---\nname: dup\ndescription: Project loses\n---\n")

	result := LoadSkills(LoadSkillsOptions{Cwd: cwd, AgentDir: agentDir, IncludeDefaults: true})
	if len(result.Skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(result.Skills))
	}
	if result.Skills[0].Description != "User wins" {
		t.Errorf("first loaded skill should win, got %q", result.Skills[0].Description)
	}

	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Type == "collision" {
			found = true
		}
	}
	if !found {
		t.Error("want a collision diagnostic")
	}
}

func TestFormatSkillsForPromptEscapesAndHides(t *testing.T) {
	prompt := FormatSkillsForPrompt([]Skill{
		{Name: "visible", Description: "Handles <html> & stuff", FilePath: "/skills/visible/SKILL.md"},
		{Name: "hidden", Description: "Explicit only", FilePath: "/skills/hidden/SKILL.md", DisableModelInvocation: true},
	})

	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("want the available_skills block")
	}
	if !strings.Contains(prompt, "Handles &lt;html&gt; &amp; stuff") {
		t.Errorf("description should be XML-escaped, got %q", prompt)
	}
	if strings.Contains(prompt, "hidden") {
		t.Error("disable-model-invocation skills should be omitted")
	}
}

func TestFormatSkillsForPromptEmptyWhenAllHidden(t *testing.T) {
	prompt := FormatSkillsForPrompt([]Skill{
		{Name: "hidden", Description: "Explicit only", DisableModelInvocation: true},
	})
	if prompt != "" {
		t.Errorf("want empty section, got %q", prompt)
	}
}
