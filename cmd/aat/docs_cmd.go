package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
)

// docsArgs holds parsed CLI flags for the docs generate command.
type docsArgs struct {
	GraphPath   string
	DomainPath  string
	NodeDocsDir string
	OutputPath  string
	Title       string
	Split       bool
}

// docsMain dispatches to docs subcommands.
func docsMain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aat docs <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands: generate")
		return 1
	}
	switch args[0] {
	case "generate":
		return docsGenerateMain(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown docs subcommand: %s\n", args[0])
		return 1
	}
}

// docsGenerateMain parses flags and delegates to docsGenerateCommand.
func docsGenerateMain(args []string) int {
	fs := flag.NewFlagSet("docs generate", flag.ContinueOnError)
	da := &docsArgs{}
	fs.StringVar(&da.GraphPath, "graph", "", "path to graph YAML file (required)")
	fs.StringVar(&da.DomainPath, "domain", "", "path to domain knowledge YAML file")
	fs.StringVar(&da.NodeDocsDir, "node-docs", "docs/nodes", "directory with per-node Markdown files")
	fs.StringVar(&da.OutputPath, "output", "workflow.md", "output file (\"-\" for stdout) or directory (with --split)")
	fs.StringVar(&da.Title, "title", "", "document title (default: derived from graph)")
	fs.BoolVar(&da.Split, "split", false, "generate directory with index.md + per-node files")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if da.GraphPath == "" {
		fmt.Fprintln(os.Stderr, "aat docs generate: --graph is required")
		return 1
	}

	if da.Split && da.OutputPath == "-" {
		fmt.Fprintln(os.Stderr, "aat docs generate: --split is incompatible with --output \"-\"")
		return 1
	}

	if err := docsGenerateCommand(da); err != nil {
		fmt.Fprintf(os.Stderr, "aat docs generate: %s\n", err)
		return 1
	}
	return 0
}

// docsGenerateCommand runs the documentation generation pipeline.
func docsGenerateCommand(args *docsArgs) error {
	// 1. Load graph
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		return fmt.Errorf("loading graph: %w", err)
	}

	// 2. Build options
	opts := &graph.DocGenOptions{
		Title: args.Title,
	}

	// 3. Load domain knowledge for enrichment (optional)
	if args.DomainPath != "" {
		kb, err := domain.ParseFile(args.DomainPath)
		if err != nil {
			return fmt.Errorf("loading domain knowledge: %w", err)
		}
		opts.Examples = buildExamples(g, kb)
	}

	// 4. Load node docs (optional, tolerates missing directory)
	nodeDocs, err := loadNodeDocs(args.NodeDocsDir, g)
	if err != nil {
		return fmt.Errorf("loading node docs: %w", err)
	}
	if len(nodeDocs) > 0 {
		opts.NodeDocs = nodeDocs
	}

	// 5. Generate and write
	if args.Split {
		return writeSplitDocs(g, opts, args.OutputPath)
	}
	return writeSingleDoc(g, opts, args.OutputPath)
}

// writeSingleDoc generates a single Markdown file.
func writeSingleDoc(g *graph.Graph, opts *graph.DocGenOptions, outputPath string) error {
	content := graph.GenerateDocs(g, opts)

	if outputPath == "-" {
		fmt.Print(content)
		return nil
	}

	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	fmt.Printf("Generated documentation: %s\n", outputPath)
	return nil
}

// writeSplitDocs generates a directory with index.md + per-node files.
func writeSplitDocs(g *graph.Graph, opts *graph.DocGenOptions, outputDir string) error {
	// Default output dir for split mode
	if outputDir == "workflow.md" {
		outputDir = "docs/api"
	}

	files := graph.GenerateDocsSplit(g, opts)

	// Create directories
	if err := os.MkdirAll(filepath.Join(outputDir, "nodes"), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Write files
	for relPath, content := range files {
		fullPath := filepath.Join(outputDir, relPath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	fmt.Printf("Generated documentation: %s/ (%d files)\n", outputDir, len(files))
	return nil
}

// loadNodeDocs reads Markdown files from docsDir and matches them to graph
// node names. Each file must be named <NodeName>.md (case-sensitive). If the
// directory does not exist, an empty map is returned with no error.
func loadNodeDocs(docsDir string, g *graph.Graph) (map[string]string, error) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	docs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		nodeName := strings.TrimSuffix(name, ".md")
		if g.Nodes[nodeName] == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(docsDir, name))
		if err != nil {
			return nil, err
		}
		docs[nodeName] = string(content)
	}
	return docs, nil
}

// buildExamples creates the enrichment map from domain knowledge.
// It uses heuristic matching to map domain concepts, types, and value pools
// to graph input fields.
func buildExamples(g *graph.Graph, kb *domain.KnowledgeBase) map[string][]string {
	if kb == nil {
		return nil
	}
	examples := make(map[string][]string)

	for nodeName, node := range g.Nodes {
		for _, inp := range node.Inputs {
			key := nodeName + "." + inp.Name

			// 1. Check domain concepts that appliesTo this input name
			vals := findConceptExamples(inp.Name, kb)

			// 2. Check domain types whose name matches input type
			if len(vals) == 0 {
				vals = findTypePoolValues(inp.Type, kb)
			}

			// 3. Check domain types whose name matches input name
			if len(vals) == 0 {
				vals = findTypePoolValues(inp.Name, kb)
			}

			// 4. Parse enum[...] types for built-in examples
			if len(vals) == 0 {
				ft, err := graph.ParseFieldType(inp.Type)
				if err == nil && ft.Kind == graph.TypeEnum {
					vals = ft.EnumValues
				}
			}

			if len(vals) > 0 {
				examples[key] = truncateExamples(vals, 5)
			}
		}
	}
	return examples
}

// findConceptExamples looks up example values from concepts that apply to the given field.
func findConceptExamples(fieldName string, kb *domain.KnowledgeBase) []string {
	concepts := kb.ConceptsForField(fieldName)
	if len(concepts) == 0 {
		return nil
	}
	// Collect example values from all matching concepts
	var vals []string
	for _, c := range concepts {
		for _, exs := range c.Examples {
			vals = append(vals, exs...)
		}
	}
	return vals
}

// findTypePoolValues finds pool values for a domain type matching the given name.
func findTypePoolValues(typeName string, kb *domain.KnowledgeBase) []string {
	td := kb.GetType(typeName)
	if td == nil || td.Pool == "" {
		return nil
	}
	return kb.AllValues(td.Pool)
}

// truncateExamples caps the number of example values.
func truncateExamples(vals []string, max int) []string {
	if len(vals) <= max {
		return vals
	}
	return vals[:max]
}
