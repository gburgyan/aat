package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gburgyan/aat/internal/version"
	"github.com/spf13/cobra"
)

// exitError wraps an error with a specific process exit code.
type exitError struct {
	Code int
	Err  error
}

func (e *exitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return ""
}

func (e *exitError) Unwrap() error { return e.Err }

var rootCmd = &cobra.Command{
	Use:   "aat",
	Short: "Adaptive API Toolkit: API workflow testing from a graph",
	Long: `Model your API as a graph once. Get long-chain integration tests, layer × environment
matrices, CI-ready runs, and an MCP server for AI coding tools — all from the same YAML.

Execution never calls an LLM. AI coding tools author plans through the MCP server;
aat prompt can draft one.`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version.Effective(), version.GitCommit, version.BuildDate),
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(promptCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(envCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(docsCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(webCmd)
	rootCmd.AddCommand(importCmd)
}

func main() {
	rootCmd.SilenceErrors = true
	if err := rootCmd.Execute(); err != nil {
		var exitErr *exitError
		if !errors.As(err, &exitErr) || (exitErr.Code != 0 && exitErr.Err != nil) {
			fmt.Fprintf(os.Stderr, "aat: %s\n", err)
		}
		os.Exit(exitCodeFor(err))
	}
}

// exitCodeFor maps the error a command returned to the process exit code. An
// exitError carries its own code: 1 when a test or validation found a failure,
// 130 when a run was aborted. Any other error means aat could not do what was
// asked (a bad flag or argument, an unknown subcommand, a project, environment,
// or setup error) and exits 2, so CI can tell a broken job from a failing test.
func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return exitCodeInfra
}

// groupRunE runs a command that only groups subcommands, such as aat run.
// Cobra shows help and exits 0 when such a command gets an argument it cannot
// match, so a mistyped subcommand (aat run bach) would pass in CI; this
// rejects it instead.
func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cmd.SilenceUsage = true
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// Cobra applies its default suggestion distance only on the root command;
	// without it, only prefixes (serv for serve) are suggested, not typos.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
	}
	return errors.New(msg)
}
