package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cappuch/maiku/codingagent"
	"github.com/cappuch/maiku/codingagent/core"
	"github.com/cappuch/maiku/codingagent/tui"
	"github.com/cappuch/maiku/updater"
	"github.com/charmbracelet/x/term"
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "update" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--check") {
			fmt.Fprintln(stderr, "Usage: maiku update [--check]")
			return 2
		}
		if err := updateCLI(updater.New("cli"), len(args) == 2, stdout); err != nil {
			fmt.Fprintln(stderr, "maiku update:", err)
			return 1
		}
		return 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--version", "-v", "version":
			version := codingagent.VERSION
			if updater.Version != "dev" {
				version += " (" + updater.Version + ")"
			}
			if _, err := fmt.Fprintf(stdout, "%s\n", version); err != nil {
				return 1
			}
			return 0
		}
	}

	return tui.Run(args, os.Stdin, stdout, stderr)
}

func main() {
	// Only interactive launches update automatically. Help/version, explicit
	// update commands, scripts, and development builds never trigger this path.
	args := os.Args[1:]
	client := updater.New("cli")
	if len(args) == 0 && term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) && client.Enabled() && updater.AutomaticEnabled() {
		enabled, settingsErr := core.AutoUpdateEnabled("")
		if settingsErr != nil {
			fmt.Fprintln(os.Stderr, "maiku: update settings:", settingsErr)
		}
		if enabled && settingsErr == nil {
			if err := updateCLI(client, false, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "maiku: update skipped:", err)
			}
		}
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func updateCLI(client *updater.Client, checkOnly bool, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	update, err := client.Check(ctx)
	cancel()
	if err != nil {
		return err
	}
	if update == nil {
		fmt.Fprintln(out, "maiku is up to date.")
		return nil
	}
	if checkOnly {
		fmt.Fprintf(out, "maiku %s is available. Run maiku update to install.\n", update.Version)
		return nil
	}
	fmt.Fprintf(out, "Installing maiku %s…\n", update.Version)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := client.Install(ctx, update); err != nil {
		return err
	}
	fmt.Fprintln(out, "Update installed; the new version will be used on your next launch.")
	return nil
}
