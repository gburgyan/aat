package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// ServerContext holds pre-loaded project state shared by all tool handlers.
type ServerContext struct {
	// Core (always available after BuildServerContext)
	Graph    *graph.Graph
	Registry *adapter.Registry
	KB       *domain.KnowledgeBase        // may be nil
	OASSpecs map[string]*v3high.Document  // spec path -> loaded doc; may be empty

	// Docs (60b, may be nil)
	DocsDir  string
	NodeDocs map[string]string // node name -> Markdown content

	// Testing lifecycle (60c, may be nil)
	PlansDir    string
	ArchiveDir  string
	Environment *config.Environment

	// Metadata
	Manifest *ProjectManifest
	GraphDir string // base dir for resolving relative paths
}

// BuildServerContext loads all project artifacts described by the manifest.
// The graph and templates are required; domain, OAS specs, and environment
// are loaded if configured. Errors in optional resources are returned as-is
// (callers should decide whether to proceed without them).
func BuildServerContext(manifest *ProjectManifest) (*ServerContext, error) {
	ctx := &ServerContext{
		Manifest: manifest,
		OASSpecs: make(map[string]*v3high.Document),
		GraphDir: filepath.Dir(manifest.GraphPath),
	}

	// Load graph (required)
	g, err := graph.ParseFile(manifest.GraphPath)
	if err != nil {
		return nil, fmt.Errorf("loading graph: %w", err)
	}
	ctx.Graph = g

	// Load templates (required)
	registry := adapter.NewRegistry()
	_, err = adapter.LoadTemplates(manifest.TemplatesPath, registry)
	if err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}
	ctx.Registry = registry

	// Load domain knowledge (optional)
	if manifest.DomainPath != "" {
		kb, err := domain.ParseFile(manifest.DomainPath)
		if err != nil {
			return nil, fmt.Errorf("loading domain knowledge: %w", err)
		}
		ctx.KB = kb
	}

	// Load OAS specs referenced by graph
	if err := ctx.loadOASSpecs(); err != nil {
		return nil, fmt.Errorf("loading OAS specs: %w", err)
	}

	// Load environment (optional)
	if manifest.EnvPath != "" {
		env, err := config.LoadEnvironment(manifest.EnvPath)
		if err != nil {
			return nil, fmt.Errorf("loading environment: %w", err)
		}
		ctx.Environment = env
	}

	// Set optional directory paths
	ctx.DocsDir = manifest.DocsDir
	ctx.PlansDir = manifest.PlansDir
	ctx.ArchiveDir = manifest.ArchiveDir

	return ctx, nil
}

// loadOASSpecs discovers and loads OAS spec files referenced in the graph.
func (ctx *ServerContext) loadOASSpecs() error {
	paths := collectSpecPaths(ctx.Graph, ctx.GraphDir)
	for _, specPath := range paths {
		if _, loaded := ctx.OASSpecs[specPath]; loaded {
			continue
		}
		doc, err := oas.LoadSpec(specPath)
		if err != nil {
			return fmt.Errorf("spec %s: %w", specPath, err)
		}
		ctx.OASSpecs[specPath] = doc
	}
	return nil
}

// collectSpecPaths returns the unique set of OAS spec file paths referenced
// by the graph, resolved relative to graphDir.
func collectSpecPaths(g *graph.Graph, graphDir string) []string {
	seen := make(map[string]bool)
	var paths []string

	addPath := func(raw string) {
		if raw == "" {
			return
		}
		resolved := raw
		if !filepath.IsAbs(raw) {
			resolved = filepath.Join(graphDir, raw)
		}
		if !seen[resolved] {
			seen[resolved] = true
			paths = append(paths, resolved)
		}
	}

	// Graph-level default
	addPath(g.OAS)

	// Node-level overrides
	for _, node := range g.Nodes {
		if node != nil && node.OAS != nil {
			addPath(node.OAS.Spec)
		}
	}

	return paths
}
