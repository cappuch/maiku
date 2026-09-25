package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cappuch/maiku/codingagent"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--version"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode=%d, want 0", exitCode)
	}
	if got, want := strings.TrimSpace(stdout.String()), codingagent.VERSION; got != want {
		t.Fatalf("stdout=%q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q, want empty", stderr.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(nil, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("exitCode=%d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "interactive mode requires a terminal") {
		t.Fatalf("stderr=%q, want terminal requirement", stderr.String())
	}
}

func TestUpdateArguments(t *testing.T) {
	for _, args := range [][]string{{"update", "--force"}, {"update", "--check", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "Usage: maiku update") {
			t.Fatalf("args=%v code=%d error=%q", args, code, stderr.String())
		}
	}
}

func TestDevelopmentUpdateDoesNotUseNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"update", "--check"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "development builds") {
		t.Fatalf("code=%d error=%q", code, stderr.String())
	}
}
