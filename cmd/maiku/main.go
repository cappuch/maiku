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
			fmt.Fprintf(stdout, "%s\n", codingagent.VERSION)
			return 0
		}
	}

	fmt.Fprintf(stderr, "%s: no CLI command implemented in this build\n", codingagent.APP_NAME)
	fmt.Fprintln(stderr, "Use --version to verify the binary.")
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
