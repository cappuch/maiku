package main

import (
	"fmt"
	"github.com/cappuch/maiku/codingagent/tui"
	"io"
	"os"

	"github.com/cappuch/maiku/codingagent"
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

	return tui.Run(args, os.Stdin, stdout, stderr)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
