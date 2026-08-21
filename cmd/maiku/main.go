package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mikus/maiku/codingagent"
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "--version", "-v", "version":
			if _, err := fmt.Fprintf(stdout, "%s\n", codingagent.VERSION); err != nil {
				return 1
			}
			return 0
		}
	}

	if _, err := fmt.Fprintf(stderr, "%s: no CLI command implemented in this build\n", codingagent.APP_NAME); err != nil {
		return 1
	}
	if _, err := fmt.Fprintln(stderr, "Use --version to verify the binary."); err != nil {
		return 1
	}
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
