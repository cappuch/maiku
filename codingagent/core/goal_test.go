package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoals(t *testing.T) {
	got := ParseGoals(" fix auth , add tests, , ship it ")
	want := []string{"fix auth", "add tests", "ship it"}
	if len(got) != len(want) {
		t.Fatalf("ParseGoals() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseGoals()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if ParseGoals("  ,  ") != nil && len(ParseGoals("  ,  ")) != 0 {
		t.Fatalf("empty input should yield no goals, got %#v", ParseGoals("  ,  "))
	}
}

func TestRenderGoalMemory(t *testing.T) {
	got := RenderGoalMemory([]string{"fix auth", "add tests"})
	for _, needle := range []string{
		"## Objectives",
		"- [ ] fix auth",
		"- [ ] add tests",
		"## Plan (draft)",
		"## Steps",
		"## Findings",
		"## Intellect",
		"## Changes",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("RenderGoalMemory missing %q:\n%s", needle, got)
		}
	}
}

func TestCreateGoalMemoryAndPrompt(t *testing.T) {
	cwd := t.TempDir()
	goals := []string{"Fix the flaky test"}
	rel, err := CreateGoalMemory(cwd, goals)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, ".maiku/goals/") || !strings.HasSuffix(rel, ".md") {
		t.Fatalf("unexpected path %q", rel)
	}
	body, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- [ ] Fix the flaky test") {
		t.Fatalf("memory missing objective:\n%s", body)
	}

	prompt := BuildGoalPrompt(rel, goals)
	for _, needle := range []string{
		"1. Fix the flaky test",
		"Temporal memory file: " + rel,
		"Plan (draft)",
		"Findings",
		"Intellect",
		"Changes",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("BuildGoalPrompt missing %q:\n%s", needle, prompt)
		}
	}
}

func TestGoalSlug(t *testing.T) {
	if got := goalSlug("Fix Auth!!!"); got != "fix-auth" {
		t.Fatalf("goalSlug = %q", got)
	}
	if got := goalSlug("!!!"); got != "goal" {
		t.Fatalf("goalSlug empty = %q", got)
	}
}
