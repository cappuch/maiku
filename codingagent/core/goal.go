package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const goalMemoryDir = ".maiku/goals"

// ParseGoals splits a /goal argument list on commas.
func ParseGoals(raw string) []string {
	parts := strings.Split(raw, ",")
	goals := make([]string, 0, len(parts))
	for _, part := range parts {
		goal := strings.TrimSpace(part)
		if goal == "" {
			continue
		}
		goals = append(goals, goal)
	}
	return goals
}

// RenderGoalMemory builds the initial temporal memory markdown for goals.
func RenderGoalMemory(goals []string) string {
	var b strings.Builder
	b.WriteString("# Goal memory\n\n")
	b.WriteString("## Objectives\n")
	for _, goal := range goals {
		fmt.Fprintf(&b, "- [ ] %s\n", goal)
	}
	b.WriteString("\n## Plan (draft)\n\n")
	b.WriteString("_Write the initial approach here before starting implementation. Refine as you work._\n\n")
	b.WriteString("## Steps\n\n")
	b.WriteString("_Ordered checklist of work. Mark items done as you go._\n\n")
	b.WriteString("## Findings\n\n")
	b.WriteString("_Facts discovered while inspecting the codebase or running commands._\n\n")
	b.WriteString("## Intellect\n\n")
	b.WriteString("_Reasoning, decisions, trade-offs, and open questions._\n\n")
	b.WriteString("## Changes\n\n")
	b.WriteString("_Concrete edits and their purpose._\n")
	return b.String()
}

// CreateGoalMemory writes a new temporal memory file under cwd/.maiku/goals/
// and returns a path relative to cwd when possible.
func CreateGoalMemory(cwd string, goals []string) (string, error) {
	if len(goals) == 0 {
		return "", fmt.Errorf("at least one goal is required")
	}
	if cwd == "" {
		cwd = "."
	}
	dir := filepath.Join(cwd, filepath.FromSlash(goalMemoryDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create goal memory dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.md", time.Now().Format("20060102-150405"), goalSlug(goals[0]))
	abs := filepath.Join(dir, name)
	if err := os.WriteFile(abs, []byte(RenderGoalMemory(goals)), 0o644); err != nil {
		return "", fmt.Errorf("write goal memory: %w", err)
	}
	rel := filepath.ToSlash(filepath.Join(goalMemoryDir, name))
	return rel, nil
}

// BuildGoalPrompt instructs the model to draft a plan into the memory file,
// then execute while continuously updating that temporal state.
func BuildGoalPrompt(memoryPath string, goals []string) string {
	var b strings.Builder
	b.WriteString("You have been given goals to complete. Work until every objective is done.\n\n")
	b.WriteString("Goals:\n")
	for i, goal := range goals {
		fmt.Fprintf(&b, "%d. %s\n", i+1, goal)
	}
	fmt.Fprintf(&b, "\nTemporal memory file: %s\n\n", memoryPath)
	b.WriteString("Workflow:\n")
	b.WriteString("1. First, write a concrete plan draft into the **Plan (draft)** section of the memory file using the write/edit tools. Do not start implementation until that draft exists.\n")
	b.WriteString("2. Then execute the plan. Continuously update the same memory file as your working state:\n")
	b.WriteString("   - **Steps** — checklist of what you are doing; check items off as you finish them\n")
	b.WriteString("   - **Findings** — discoveries from reading code or running commands\n")
	b.WriteString("   - **Intellect** — reasoning, decisions, trade-offs, open questions\n")
	b.WriteString("   - **Changes** — concrete edits and why\n")
	b.WriteString("   - **Objectives** — mark each goal `[x]` when verified complete\n")
	b.WriteString("3. Prefer the memory file over chat history for tracking progress. Keep it current after meaningful work.\n")
	b.WriteString("4. Act autonomously until the goals are achieved, then give a short summary of what changed.\n")
	return b.String()
}

func goalSlug(goal string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(goal) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if b.Len() == 0 || lastHyphen {
				continue
			}
			b.WriteByte('-')
			lastHyphen = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "goal"
	}
	return slug
}
