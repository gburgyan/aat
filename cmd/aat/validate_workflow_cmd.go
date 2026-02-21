package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/spf13/cobra"
)

// validateWorkflowCmd is the Cobra command for workflow validation under "aat validate workflow".
var validateWorkflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Validate workflows against a graph",
	Long:  "Validate workflow compatibility and workflow files against a graph definition.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		// Build overrides from explicitly-set flags only
		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("graph") {
			overrides.GraphPath, _ = cmd.Flags().GetString("graph")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		strict, _ := cmd.Flags().GetBool("strict")

		wa := &workflowValidateArgs{
			GraphPath:    resolved.GraphPath,
			WorkflowsDir: resolved.WorkflowsDir,
			Strict:       strict,
		}

		code := workflowValidateCommand(wa, os.Stdout)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	validateCmd.AddCommand(validateWorkflowCmd)

	validateWorkflowCmd.Flags().String("graph", "", "path to graph YAML file")
	validateWorkflowCmd.Flags().Bool("strict", false, "treat warnings as errors")
}

// workflowValidateArgs holds parsed CLI flags for workflow validate.
type workflowValidateArgs struct {
	GraphPath    string
	WorkflowsDir string
	Strict       bool
}

// workflowValidateCommand runs workflow validation against a graph.
// It checks workflow compatibility (inline definitions) and validates
// workflow files in the workflows directory.
// Extracted for testability.
func workflowValidateCommand(args *workflowValidateArgs, out io.Writer) int {
	if args.GraphPath == "" {
		fmt.Fprintln(os.Stderr, "aat validate workflow: --graph is required")
		return 1
	}

	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat validate workflow: %s\n", err)
		return 1
	}

	var sections []sectionResult

	// 1. Workflow compatibility (inline workflow definitions in the graph)
	if len(g.Workflows) > 0 {
		graphDir := filepath.Dir(args.GraphPath)
		compatResult := intent.ValidateWorkflowCompat(g, graphDir)

		if compatResult.HasErrors() || (args.Strict && compatResult.HasIssues()) {
			var wfErrors []string
			for _, e := range compatResult.Errors {
				wfErrors = append(wfErrors, fmt.Sprintf("workflow %q: %s", e.Workflow, e.Err))
			}
			if compatResult.HasWarnings() || compatResult.HasNonProducible() {
				wfErrors = append(wfErrors, compatResult.Format())
			}
			sections = append(sections, sectionResult{
				Name:   "Workflow compatibility",
				Status: "FAILED",
				Errors: wfErrors,
			})
		} else {
			sections = append(sections, sectionResult{
				Name:   "Workflow compatibility",
				Status: "OK",
				Detail: fmt.Sprintf("(%d workflows)", len(g.Workflows)),
			})
		}
	}

	// 2. Workflow files validation
	if args.WorkflowsDir != "" {
		graphDir := filepath.Dir(args.GraphPath)
		wfTemplates := workflowTemplatePaths(g, graphDir)
		pvr := validateWorkflows(args.WorkflowsDir, g, wfTemplates)
		if len(pvr.Errors) > 0 {
			sections = append(sections, sectionResult{
				Name:   "Workflows",
				Status: "FAILED",
				Errors: pvr.Errors,
			})
		} else if pvr.Total > 0 {
			detail := fmt.Sprintf("(%d files", pvr.Total)
			if pvr.Templates > 0 {
				detail += fmt.Sprintf(", %d templates", pvr.Templates)
			}
			detail += ")"
			sections = append(sections, sectionResult{
				Name:   "Workflows",
				Status: "OK",
				Detail: detail,
			})
		}
	}

	if len(sections) == 0 {
		fmt.Fprintln(out, "No workflows found.")
		return 0
	}

	printSections(out, sections)

	for _, s := range sections {
		if s.Status == "FAILED" {
			return 1
		}
	}
	return 0
}
