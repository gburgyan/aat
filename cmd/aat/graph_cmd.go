package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/intent"
	"github.com/spf13/cobra"
)

// graphCmd is the parent Cobra command for graph subcommands.
var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Graph inspection and validation commands",
}

// graphValidateCmd is the Cobra command for graph validation.
var graphValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a graph definition",
	Long:  "Validate graph structure, OAS spec consistency, adapter outputs, and workflow compatibility.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		// Build overrides from explicitly-set flags only
		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("graph") {
			overrides.GraphPath, _ = cmd.Flags().GetString("graph")
		}
		if cmd.Flags().Changed("templates") {
			overrides.TemplatesPath, _ = cmd.Flags().GetString("templates")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		oasPath, _ := cmd.Flags().GetString("oas")
		strict, _ := cmd.Flags().GetBool("strict")

		ga := &graphValidateArgs{
			GraphPath:     resolved.GraphPath,
			OASPath:       oasPath,
			TemplatesPath: resolved.TemplatesPath,
			Strict:        strict,
		}

		code := graphValidateCommand(ga)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	graphCmd.AddCommand(graphValidateCmd)

	graphValidateCmd.Flags().String("graph", "", "path to graph YAML file")
	graphValidateCmd.Flags().String("oas", "", "path to OAS spec file (overrides graph-level oas)")
	graphValidateCmd.Flags().String("templates", "", "path to templates directory (validates outputs match extracts)")
	graphValidateCmd.Flags().Bool("strict", false, "treat warnings as errors")
}

// graphValidateArgs holds parsed CLI flags for graph validate.
type graphValidateArgs struct {
	GraphPath     string
	OASPath       string
	TemplatesPath string
	Strict        bool
}

// graphValidateCommand runs structural, OAS, and adapter output validation.
// Extracted for testability.
func graphValidateCommand(args *graphValidateArgs) int {
	if args.GraphPath == "" {
		fmt.Fprintln(os.Stderr, "aat graph validate: --graph is required")
		return 1
	}

	hasError := false

	// 1. Parse graph (runs structural validation)
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat graph validate: %s\n", err)
		return 1
	}
	fmt.Printf("Graph structure: OK (%d nodes)\n", len(g.Nodes))

	// 2. Apply OAS flag override before collecting spec paths
	if args.OASPath != "" {
		g.OAS = args.OASPath
	}

	// 3. Collect and load OAS specs via SpecValidator
	validator := oas.NewValidator()
	specPaths := validator.CollectSpecPaths(g)
	if len(specPaths) > 0 {
		graphDir := filepath.Dir(args.GraphPath)
		for _, sp := range specPaths {
			// Resolve relative to graph file directory
			resolvedPath := sp
			if !filepath.IsAbs(sp) {
				resolvedPath = filepath.Join(graphDir, sp)
			}
			if err := validator.LoadSpec(sp, resolvedPath); err != nil {
				fmt.Fprintf(os.Stderr, "aat graph validate: loading OAS spec %q: %s\n", sp, err)
				return 1
			}
		}

		// 4. Run OAS validation
		result := validator.Validate(g)
		if result.HasIssues() {
			fmt.Println()
			fmt.Println(result.Format())
			if result.HasErrors() {
				hasError = true
			}
			if args.Strict {
				hasError = true
			}
		} else {
			fmt.Println("OAS validation: OK")
		}
	}

	// 5. Adapter output validation (when --templates is provided)
	if args.TemplatesPath != "" {
		registry := adapter.NewRegistry()
		n, err := adapter.LoadTemplates(args.TemplatesPath, registry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aat graph validate: loading templates: %s\n", err)
			return 1
		}

		if err := engine.ValidateAdapterOutputs(g, registry); err != nil {
			fmt.Println()
			fmt.Println(err)
			hasError = true
		} else {
			fmt.Printf("Adapter outputs: OK (%d templates)\n", n)
		}
	}

	// 6. Workflow compatibility validation
	if len(g.Workflows) > 0 {
		graphDir := filepath.Dir(args.GraphPath)
		compatResult := intent.ValidateWorkflowCompat(g, graphDir)

		if len(compatResult.Errors) > 0 {
			for _, e := range compatResult.Errors {
				fmt.Fprintf(os.Stderr, "workflow %q: template error: %s\n", e.Workflow, e.Err)
			}
		}

		if compatResult.HasWarnings() || compatResult.HasNonProducible() {
			fmt.Println()
			fmt.Println(compatResult.Format())
			if args.Strict && compatResult.HasWarnings() {
				hasError = true
			}
		} else if !compatResult.HasErrors() {
			fmt.Printf("Workflow compatibility: OK (%d workflows)\n", len(g.Workflows))
		}
	}

	if hasError {
		return 1
	}
	return 0
}
