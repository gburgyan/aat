package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// generateCmd is the Cobra command for scaffolding from an OAS spec.
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate graph and templates from an OAS spec",
	Long:  "Scaffold a graph definition and request templates from an OpenAPI specification.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		oasPath, _ := cmd.Flags().GetString("oas")
		outputGraph, _ := cmd.Flags().GetString("output-graph")
		outputTemplates, _ := cmd.Flags().GetString("output-templates")

		if oasPath == "" {
			return fmt.Errorf("--oas is required")
		}

		ga := &generateArgs{
			OASPath:         oasPath,
			OutputGraph:     outputGraph,
			OutputTemplates: outputTemplates,
		}

		return generateCommand(ga)
	},
}

func init() {
	generateCmd.Flags().String("oas", "", "path to OAS spec file (required)")
	generateCmd.Flags().String("output-graph", "graph.yaml", "output path for graph YAML (\"-\" for stdout)")
	generateCmd.Flags().String("output-templates", "templates", "output directory for template YAML files")
}

// generateArgs holds parsed CLI flags for the generate command.
type generateArgs struct {
	OASPath         string
	OutputGraph     string
	OutputTemplates string
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
