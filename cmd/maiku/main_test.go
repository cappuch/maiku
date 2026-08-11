package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mikus/maiku/codingagent"
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
	if !strings.Contains(stderr.String(), codingagent.APP_NAME+": no CLI command implemented in this build") {
		t.Fatalf("stderr=%q, want unsupported message", stderr.String())
	}
}
