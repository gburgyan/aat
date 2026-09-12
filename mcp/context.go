package mcp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// ServerContext holds pre-loaded project state shared by all tool handlers.
type ServerContext struct {
	// Core (always available after BuildServerContext)
	Graph    *graph.Graph
	Registry *adapter.Registry
	KB       *domain.KnowledgeBase       // may be nil
	OASSpecs map[string]*v3high.Document // spec path -> loaded doc; may be empty

	// Docs (60b, may be nil)
	DocsDir  string
	NodeDocs map[string]string // node name -> Markdown content

	// Testing lifecycle (60c, may be nil)
	WorkflowsDir string
	PlanDirs     []string
	LayersDir    string // resolves recipe layers; empty when the manifest sets none
	ArchiveDir   string
	Environment  *config.Environment
	AuthProvider *config.AuthProvider // cached default auth (nil if no environment)
	// Pacer spaces execute_plan requests by the environment's
	// settings.minRequestInterval, across calls (nil = no pacing).
	Pacer *engine.Pacer

	// Metadata
	Manifest      *ProjectManifest
	GraphDir      string // base dir for resolving relative paths
	ReadmeContent string // README.md content from graph directory, if found
}

// BuildServerContext loads all project artifacts described by the manifest.
// The graph and templates are required; domain, OAS specs, and environment
// are loaded if configured. Errors in optional resources are returned as-is
// (callers should decide whether to proceed without them).
func BuildServerContext(manifest *ProjectManifest) (*ServerContext, error) {
	return BuildServerContextWithVars(manifest, nil)
}

// BuildServerContextWithVars is BuildServerContext with vars set from outside
// the environment file, such as --var flags; see
// config.LoadNamedEnvironmentWithVars.
func BuildServerContextWithVars(manifest *ProjectManifest, vars map[string]string) (*ServerContext, error) {
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
		env, err := config.LoadNamedEnvironmentWithVars(manifest.EnvPath, manifest.DefaultEnvironment, vars)
		if err != nil {
			return nil, fmt.Errorf("loading environment: %w", err)
		}
		ctx.Environment = env
		ctx.AuthProvider = config.NewAuthProvider(env.Auth)
		interval, err := env.Settings.RequestInterval()
		if err != nil {
			return nil, fmt.Errorf("loading environment: settings.minRequestInterval: %w", err)
		}
		ctx.Pacer = engine.NewPacer(interval)
	}

	// Set optional directory paths
	ctx.DocsDir = manifest.DocsDir
	ctx.WorkflowsDir = manifest.WorkflowsDir
	ctx.PlanDirs = []string(manifest.PlanDirs)
	ctx.LayersDir = manifest.LayersDir
	ctx.ArchiveDir = manifest.ArchiveDir

	// Load node documentation (optional)
	if ctx.DocsDir != "" {
		nodeDocs, err := loadNodeDocs(ctx.DocsDir, ctx.Graph)
		if err != nil {
			return nil, fmt.Errorf("loading node docs: %w", err)
		}
		ctx.NodeDocs = nodeDocs
	}

	// Load README.md from graph directory (optional, convention-based)
	readmePath := filepath.Join(ctx.GraphDir, "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		ctx.ReadmeContent = string(data)
	}

	return ctx, nil
}

// reconstitute rebuilds a recipe into a full plan, loading its layers from the
// manifest's layers directory. intent.Reconstitute rejects recipe layers when
// no directory is configured.
func (ctx *ServerContext) reconstitute(r *plan.Recipe) (*plan.Plan, error) {
	return intent.Reconstitute(r, ctx.Graph, ctx.GraphDir, intent.WithLayersDir(ctx.LayersDir))
}

// layeredDefaults stacks the named layers on the graph defaults for execution.
func (ctx *ServerContext) layeredDefaults(layers []string) (map[string]*graph.InputDefault, error) {
	return graph.LayeredDefaults(ctx.Graph, layers, ctx.LayersDir)
}

// loadOASSpecs discovers and loads OAS spec files referenced in the manifest and graph.
func (ctx *ServerContext) loadOASSpecs() error {
	paths := collectSpecPaths(ctx.Graph, ctx.GraphDir, ctx.Manifest)
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
// by the manifest and graph. Graph references resolve against graphDir;
// manifest paths arrive resolved against the manifest's directory.
func collectSpecPaths(g *graph.Graph, graphDir string, manifest *config.ProjectManifest) []string {
	seen := make(map[string]bool)
	var paths []string

	addPath := func(raw, baseDir string) {
		if raw == "" {
			return
		}
		resolved := raw
		if !filepath.IsAbs(raw) {
			resolved = filepath.Join(baseDir, raw)
		}
		if !seen[resolved] {
			seen[resolved] = true
			paths = append(paths, resolved)
		}
	}

	// Manifest-declared OAS paths, already resolved against the manifest's
	// directory by LoadManifest. Joining them onto graphDir again doubled a
	// relative prefix (examples/shop/examples/shop/openapi.yaml) when the
	// manifest was loaded through a relative path.
	if manifest != nil {
		for _, p := range manifest.OASPaths {
			addPath(p, "")
		}
	}

	// Graph-level default
	addPath(g.OAS, graphDir)

	// Node-level overrides
	for _, node := range g.Nodes {
		if node != nil && node.OAS != nil {
			addPath(node.OAS.Spec, graphDir)
		}
	}

	return paths
}
