package main

import (
	"errors"
	"fmt"
	"os"

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
	Use:     "aat",
	Short:   "Adaptive API Testing",
	Long:    "AAT is a CLI tool that uses LLM-assisted planning and execution to test API workflows end-to-end.",
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version.Version, version.GitCommit, version.BuildDate),
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(promptCmd)
	rootCmd.AddCommand(graphCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(docsCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(validateCmd)
}

func main() {
	rootCmd.SilenceErrors = true
	if err := rootCmd.Execute(); err != nil {
		var exitErr *exitError
		if errors.As(err, &exitErr) {
			if exitErr.Code != 0 && exitErr.Err != nil {
				fmt.Fprintf(os.Stderr, "aat: %s\n", exitErr.Err)
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "aat: %s\n", err)
		os.Exit(1)
	}
}
