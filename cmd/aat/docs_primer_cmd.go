package main

import (
	"io"

	"github.com/gburgyan/aat/internal/primer"
	"github.com/spf13/cobra"
)

// docsPrimerCmd prints the primer for AI coding assistants.
var docsPrimerCmd = &cobra.Command{
	Use:   "primer",
	Short: "Print the primer for AI coding assistants, as Markdown",
	Long: `Print the primer that AI coding assistants read to author and run an AAT
project: the project files, the edit, validate, run loop, and how to read what a
run recorded. It is the Markdown the docs site publishes as llms-full.txt, in the
version that matches this binary.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := io.WriteString(cmd.OutOrStdout(), primer.Markdown())
		return err
	},
}

func init() {
	docsCmd.AddCommand(docsPrimerCmd)
}
