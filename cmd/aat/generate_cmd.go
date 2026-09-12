package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/graph/oas"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// generateCmd is the Cobra command for scaffolding from an OAS spec.
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate graph and templates from an OAS spec",
	Long:  "Scaffold a graph definition and request templates from an OpenAPI specification.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		oasPath, _ := cmd.Flags().GetString("oas")
		outputGraph, _ := cmd.Flags().GetString("output-graph")
		outputTemplates, _ := cmd.Flags().GetString("output-templates")
		force, _ := cmd.Flags().GetBool("force")

		if oasPath == "" {
			return fmt.Errorf("--oas is required")
		}

		ga := &generateArgs{
			OASPath:           oasPath,
			OutputGraph:       outputGraph,
			OutputTemplates:   outputTemplates,
			TemplatesExplicit: cmd.Flags().Changed("output-templates"),
			Force:             force,
		}

		return generateCommand(ga)
	},
}

func init() {
	generateCmd.Flags().String("oas", "", "path to OAS spec file (required)")
	generateCmd.Flags().String("output-graph", "graph.yaml", "output path for graph YAML (\"-\" for stdout)")
	generateCmd.Flags().String("output-templates", "templates", "output directory for template YAML files (not written with --output-graph - unless given)")
	generateCmd.Flags().Bool("force", false, "replace an existing graph file and templates")
}

// generateArgs holds parsed CLI flags for the generate command.
type generateArgs struct {
	OASPath           string
	OutputGraph       string
	OutputTemplates   string
	TemplatesExplicit bool // --output-templates was given; templates are written even with --output-graph -
	Force             bool // replace files that already exist
}

// generateCommand runs the scaffold generation pipeline. Extracted for testability.
func generateCommand(args *generateArgs) error {
	// Load OAS spec
	model, err := oas.LoadSpec(args.OASPath)
	if err != nil {
		return fmt.Errorf("loading spec: %w", err)
	}

	// Generate scaffold
	result, err := oas.Generate(model, specReference(args.OASPath, args.OutputGraph))
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

	// Previewing the graph on stdout writes no templates unless a templates
	// directory was asked for explicitly.
	writeTemplates := args.OutputGraph != "-" || args.TemplatesExplicit
	var targets []string
	if args.OutputGraph != "-" {
		targets = append(targets, args.OutputGraph)
	}
	templatePaths := make(map[string]string, len(result.Templates))
	if writeTemplates {
		byFoldedName := make(map[string]string, len(result.Templates))
		for _, tmpl := range result.Templates {
			folded := strings.ToLower(tmpl.Adapter)
			if other, clash := byFoldedName[folded]; clash {
				return fmt.Errorf("operationIds %q and %q differ only in case: their templates would be one file on a case-insensitive file system", other, tmpl.Adapter)
			}
			byFoldedName[folded] = tmpl.Adapter
			templatePaths[tmpl.Adapter] = filepath.Join(args.OutputTemplates, tmpl.Adapter+".yaml")
			targets = append(targets, templatePaths[tmpl.Adapter])
		}
	}

	// Check every file before writing any, so a refused run changes nothing.
	if !args.Force {
		if err := refuseOverwrite(targets); err != nil {
			return err
		}
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
	if !writeTemplates {
		return nil
	}
	if err := os.MkdirAll(args.OutputTemplates, 0755); err != nil {
		return fmt.Errorf("creating templates directory: %w", err)
	}
	for _, tmpl := range result.Templates {
		data, err := yaml.Marshal(tmpl)
		if err != nil {
			return fmt.Errorf("marshaling template %s: %w", tmpl.Adapter, err)
		}
		if err := os.WriteFile(templatePaths[tmpl.Adapter], data, 0644); err != nil {
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

// refuseOverwrite returns an error naming the paths that already exist (the
// first five, and a count of the rest), or nil when none do.
func refuseOverwrite(paths []string) error {
	var existing []string
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return nil
	}
	shown, rest := existing, ""
	if len(existing) > 5 {
		shown = existing[:5]
		rest = fmt.Sprintf(" and %d more", len(existing)-5)
	}
	return fmt.Errorf("refusing to overwrite %s: %s%s; use --force to replace them",
		pluralize(len(existing), "existing file"), strings.Join(shown, ", "), rest)
}

// specReference returns the graph's oas: reference to specPath: the path from
// the directory the graph is written to (the working directory for "-"), with
// forward slashes. It falls back to the absolute path when there is no relative
// one, such as across Windows volumes.
func specReference(specPath, outputGraph string) string {
	spec, err := filepath.Abs(specPath)
	if err != nil {
		return filepath.ToSlash(specPath)
	}
	dir := "."
	if outputGraph != "-" {
		dir = filepath.Dir(outputGraph)
	}
	graphDir, err := filepath.Abs(dir)
	if err != nil {
		return filepath.ToSlash(spec)
	}
	rel, err := filepath.Rel(graphDir, spec)
	if err != nil {
		return filepath.ToSlash(spec)
	}
	return filepath.ToSlash(rel)
}
