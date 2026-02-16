package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/intent"
)

// graphMain dispatches to graph subcommands.
func graphMain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aat graph <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands: validate")
		return 1
	}
	switch args[0] {
	case "validate":
		return graphValidateMain(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown graph subcommand: %s\n", args[0])
		return 1
	}
}

// graphValidateArgs holds parsed CLI flags for graph validate.
type graphValidateArgs struct {
	GraphPath     string
	OASPath       string
	TemplatesPath string
	Strict        bool
}

// graphValidateMain parses flags and runs graph validation.
func graphValidateMain(args []string) int {
	fs := flag.NewFlagSet("graph validate", flag.ContinueOnError)
	ga := &graphValidateArgs{}
	fs.StringVar(&ga.GraphPath, "graph", "", "path to graph YAML file (required)")
	fs.StringVar(&ga.OASPath, "oas", "", "path to OAS spec file (overrides graph-level oas)")
	fs.StringVar(&ga.TemplatesPath, "templates", "", "path to templates directory (validates outputs match extracts)")
	fs.BoolVar(&ga.Strict, "strict", false, "treat warnings as errors")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	return graphValidateCommand(ga)
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
	fmt.Printf("Graph structure: OK (%d nodes, %d edges)\n", len(g.Nodes), len(g.Edges))

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
