package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/graph/oas"
	"gopkg.in/yaml.v3"
)

// generateArgs holds parsed CLI flags for the generate command.
type generateArgs struct {
	OASPath         string
	OutputGraph     string
	OutputTemplates string
}

// generateMain parses flags and delegates to generateCommand.
func generateMain(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	ga := &generateArgs{}
	fs.StringVar(&ga.OASPath, "oas", "", "path to OAS spec file (required)")
	fs.StringVar(&ga.OutputGraph, "output-graph", "graph.yaml", "output path for graph YAML (\"-\" for stdout)")
	fs.StringVar(&ga.OutputTemplates, "output-templates", "templates", "output directory for template YAML files")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if ga.OASPath == "" {
		fmt.Fprintln(os.Stderr, "aat generate: --oas is required")
		return 1
	}

	if err := generateCommand(ga); err != nil {
		fmt.Fprintf(os.Stderr, "aat generate: %s\n", err)
		return 1
	}
	return 0
}

// generateCommand runs the scaffold generation pipeline. Extracted for testability.
func generateCommand(args *generateArgs) error {
	// Load OAS spec
	model, err := oas.LoadSpec(args.OASPath)
	if err != nil {
		return fmt.Errorf("loading spec: %w", err)
	}

	// Generate scaffold
	specFile := filepath.Base(args.OASPath)
	result, err := oas.Generate(model, specFile)
	if err != nil {
		return fmt.Errorf("generating scaffold: %w", err)
	}

	// Print warnings to stderr
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	// Marshal graph
	graphData, err := yaml.Marshal(result.Graph)
	if err != nil {
		return fmt.Errorf("marshaling graph: %w", err)
	}

	// Write graph
	if args.OutputGraph == "-" {
		fmt.Print(string(graphData))
	} else {
		if err := os.WriteFile(args.OutputGraph, graphData, 0644); err != nil {
			return fmt.Errorf("writing graph: %w", err)
		}
	}

	// Write templates
	if err := os.MkdirAll(args.OutputTemplates, 0755); err != nil {
		return fmt.Errorf("creating templates directory: %w", err)
	}
	for _, tmpl := range result.Templates {
		data, err := yaml.Marshal(tmpl)
		if err != nil {
			return fmt.Errorf("marshaling template %s: %w", tmpl.Adapter, err)
		}
		path := filepath.Join(args.OutputTemplates, tmpl.Adapter+".yaml")
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("writing template %s: %w", tmpl.Adapter, err)
		}
	}

	// Print summary
	if args.OutputGraph != "-" {
		fmt.Printf("Generated %d nodes, %d templates written to %s\n",
			len(result.Graph.Nodes), len(result.Templates), args.OutputTemplates)
	}

	return nil
}
