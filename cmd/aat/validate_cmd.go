package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/graph/oas"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/plan"
	"github.com/spf13/cobra"
)

// validateCmd is the Cobra command for project validation.
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the current AAT project",
	Long:  "Validate the AAT project: manifest, environments, domain, visualizers, graph, OAS specs, templates, workflows, layers, and plans. Unknown keys in any project file are errors.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		manifestFlag, _ := cmd.Flags().GetString("manifest")
		strict, _ := cmd.Flags().GetBool("strict")
		varFlags, _ := cmd.Flags().GetStringArray("var")
		vars, err := config.ParseVars(varFlags)
		if err != nil {
			return err
		}

		va := &validateArgs{
			ManifestPath: manifestFlag,
			Strict:       strict,
			Vars:         vars,
		}

		code := validateCommand(va, os.Stdout)
		if code != 0 {
			return &exitError{Code: code}
		}
		return nil
	},
}

func init() {
	validateCmd.Flags().String("manifest", "", "explicit path to aat-project.yaml (auto-discovered if omitted)")
	validateCmd.Flags().Bool("strict", false, "treat warnings as errors")
	validateCmd.Flags().StringArray("var", nil, "set a var of a multi-environment file, KEY=VALUE (repeatable; wins over the file's vars)")
}

// validateArgs holds parsed CLI flags for the validate command.
type validateArgs struct {
	ManifestPath string
	Strict       bool
	Vars         map[string]string // --var KEY=VALUE for multi-environment files
}

// sectionResult tracks the outcome of one validation section.
type sectionResult struct {
	Name   string
	Status string // "OK", "WARN" (issues that fail only under --strict), "FAILED"
	Detail string // e.g. "(59 nodes)"
	Errors []string
}

// validateCommand runs full project validation. It returns 0 when the project
// is valid, 1 when validation finds a problem (including a manifest that fails
// to load), and 2 when there is no manifest to validate.
func validateCommand(args *validateArgs, out io.Writer) int {
	var sections []sectionResult

	// 1. Discover manifest via ResolveProjectPaths (respects AAT_PROJECT env,
	//    user home config, CWD walk-up, and --manifest flag).
	overrides := config.ProjectPaths{}
	if args.ManifestPath != "" {
		overrides.ExplicitManifest = args.ManifestPath
	}
	resolved, err := config.ResolveProjectPaths(overrides)
	if err != nil || resolved.ManifestPath == "" {
		errMsg := "no manifest found (checked AAT_PROJECT, CWD walk-up, and user config)"
		if err != nil {
			errMsg = err.Error()
		}
		sections = append(sections, sectionResult{
			Name:   "Manifest",
			Status: "FAILED",
			Errors: []string{errMsg},
		})
		printSections(out, sections)
		if err == nil || errors.Is(err, config.ErrManifestNotFound) {
			return exitCodeInfra
		}
		return 1
	}

	m, err := config.LoadManifest(resolved.ManifestPath)
	if err != nil {
		sections = append(sections, sectionResult{
			Name:   "Manifest",
			Status: "FAILED",
			Errors: []string{err.Error()},
		})
		printSections(out, sections)
		return 1
	}
	shortenManifestPaths(m)

	// Validate referenced files exist on disk
	var manifestErrors []string
	if _, err := os.Stat(m.GraphPath); err != nil {
		manifestErrors = append(manifestErrors, fmt.Sprintf("graph file not found: %s", m.GraphPath))
	}
	if _, err := os.Stat(m.TemplatesPath); err != nil {
		manifestErrors = append(manifestErrors, fmt.Sprintf("templates dir not found: %s", m.TemplatesPath))
	}
	if m.DomainPath != "" {
		if _, err := os.Stat(m.DomainPath); err != nil {
			manifestErrors = append(manifestErrors, fmt.Sprintf("domain file not found: %s", m.DomainPath))
		}
	}
	if m.EnvPath != "" {
		if _, err := os.Stat(m.EnvPath); err != nil {
			manifestErrors = append(manifestErrors, fmt.Sprintf("environment file not found: %s", m.EnvPath))
		}
	}
	if m.WorkflowsDir != "" {
		if _, err := os.Stat(m.WorkflowsDir); err != nil {
			manifestErrors = append(manifestErrors, fmt.Sprintf("workflows dir not found: %s", m.WorkflowsDir))
		}
	}
	if m.LayersDir != "" {
		if _, err := os.Stat(m.LayersDir); err != nil {
			manifestErrors = append(manifestErrors, fmt.Sprintf("layers dir not found: %s", m.LayersDir))
		}
	}
	for _, pd := range m.PlanDirs {
		if _, err := os.Stat(pd); err != nil {
			manifestErrors = append(manifestErrors, fmt.Sprintf("plans dir not found: %s", pd))
		}
	}

	if len(manifestErrors) > 0 {
		sections = append(sections, sectionResult{
			Name:   "Manifest",
			Status: "FAILED",
			Errors: manifestErrors,
		})
		printSections(out, sections)
		return 1
	}

	detail := ""
	if m.Name != "" {
		detail = fmt.Sprintf("(project: %s)", m.Name)
	}
	sections = append(sections, sectionResult{
		Name:   "Manifest",
		Status: "OK",
		Detail: detail,
	})

	// 2. Files that stand alone: environment, domain, visualizers
	if m.EnvPath != "" {
		envSection, unusableVars := validateEnvironmentFile(m.EnvPath, m.DefaultEnvironment, args.Vars)
		sections = append(sections, *envSection)
		// A --var the environment file cannot use is a mistake in the
		// invocation, like a missing manifest: report it and stop.
		if unusableVars {
			printSections(out, sections)
			return exitCodeInfra
		}
	}
	if m.DomainPath != "" {
		sections = append(sections, validateDomain(m.DomainPath))
	}
	if m.VisualizersDir != "" {
		sections = append(sections, validateVisualizers(m.VisualizersDir))
	}

	// 3. Parse graph
	g, err := graph.ParseFile(m.GraphPath)
	if err != nil {
		sections = append(sections, sectionResult{
			Name:   "Graph structure",
			Status: "FAILED",
			Errors: []string{err.Error()},
		})
		printSections(out, sections)
		return 1
	}
	// Cleanup when conditions are predicates, which the plan package checks.
	if condErrs := plan.ValidateCleanupConditions(g); len(condErrs) > 0 {
		sections = append(sections, sectionResult{
			Name:   "Graph structure",
			Status: "FAILED",
			Errors: []string{(&graph.ValidationError{Errors: condErrs}).Error()},
		})
		printSections(out, sections)
		return 1
	}
	sections = append(sections, sectionResult{
		Name:   "Graph structure",
		Status: "OK",
		Detail: "(" + pluralize(len(g.Nodes), "node") + ")",
	})

	// Templates feed the OAS output check (outputs are looked up at their
	// extract paths) as well as the template sections after it.
	registry := adapter.NewRegistry()
	templateCount, templateErr := adapter.LoadTemplates(m.TemplatesPath, registry)

	// 3. OAS validation
	validator := oas.NewValidator()
	if templateErr == nil {
		validator.WithOutputPaths(engine.OutputExtractPaths(g, registry)).
			WithSuppliedFields(engine.TemplateSuppliedFields(g, registry)).
			WithHeaderInputs(engine.TemplateHeaderInputs(g, registry))
	}
	specPaths := validator.CollectSpecPaths(g)
	if len(specPaths) > 0 {
		graphDir := filepath.Dir(m.GraphPath)
		var oasErrors []string
		loadFailed := false
		for _, sp := range specPaths {
			resolvedPath := sp
			if !filepath.IsAbs(sp) {
				resolvedPath = filepath.Join(graphDir, sp)
			}
			if err := validator.LoadSpec(sp, resolvedPath); err != nil {
				oasErrors = append(oasErrors, fmt.Sprintf("loading spec %q: %s", sp, err))
				loadFailed = true
			}
		}
		if loadFailed {
			sections = append(sections, sectionResult{
				Name:   "OAS validation",
				Status: "FAILED",
				Errors: oasErrors,
			})
		} else {
			result := validator.Validate(g)
			if result.HasIssues() {
				sections = append(sections, sectionResult{
					Name:   "OAS validation",
					Status: issueStatus(result.HasErrors(), args.Strict),
					Errors: []string{result.Format()},
				})
			} else {
				sections = append(sections, sectionResult{
					Name:   "OAS validation",
					Status: "OK",
				})
			}
		}
	}

	// 4. Adapter outputs
	if templateErr != nil {
		sections = append(sections, sectionResult{
			Name:   "Adapter outputs",
			Status: "FAILED",
			Errors: []string{templateErr.Error()},
		})
	} else if err := engine.ValidateAdapterOutputs(g, registry); err != nil {
		sections = append(sections, sectionResult{
			Name:   "Adapter outputs",
			Status: "FAILED",
			Errors: []string{err.Error()},
		})
	} else {
		sections = append(sections, sectionResult{
			Name:   "Adapter outputs",
			Status: "OK",
			Detail: "(" + pluralize(templateCount, "template") + ")",
		})
	}

	// 5. Template inputs — check required placeholders vs optional graph inputs
	if err := engine.ValidateTemplateInputs(g, registry); err != nil {
		sections = append(sections, sectionResult{
			Name:   "Template inputs",
			Status: "FAILED",
			Errors: []string{err.Error()},
		})
	} else {
		sections = append(sections, sectionResult{
			Name:   "Template inputs",
			Status: "OK",
		})
	}

	// 6. Workflow compatibility
	if len(g.Workflows) > 0 {
		graphDir := filepath.Dir(m.GraphPath)
		compatResult := intent.ValidateWorkflowCompat(g, graphDir)

		if compatResult.HasErrors() || compatResult.HasIssues() {
			var wfErrors []string
			for _, e := range compatResult.Errors {
				wfErrors = append(wfErrors, fmt.Sprintf("workflow %q: %s", e.Workflow, e.Err))
			}
			if compatResult.HasWarnings() || compatResult.HasNonProducible() {
				wfErrors = append(wfErrors, compatResult.Format())
			}
			sections = append(sections, sectionResult{
				Name:   "Workflow compatibility",
				Status: issueStatus(compatResult.HasErrors(), args.Strict),
				Errors: wfErrors,
			})
		} else {
			sections = append(sections, sectionResult{
				Name:   "Workflow compatibility",
				Status: "OK",
				Detail: "(" + pluralize(len(g.Workflows), "workflow") + ")",
			})
		}
	}

	// 7. Workflows validation
	if m.WorkflowsDir != "" {
		graphDir := filepath.Dir(m.GraphPath)
		wfTemplates := workflowTemplatePaths(g, graphDir)
		pvr := validateWorkflows(m.WorkflowsDir, g, wfTemplates)
		if len(pvr.Errors) > 0 {
			sections = append(sections, sectionResult{
				Name:   "Workflows",
				Status: "FAILED",
				Errors: pvr.Errors,
			})
		} else if pvr.Total > 0 {
			detail := "(" + pluralize(pvr.Total, "file")
			if pvr.Templates > 0 {
				detail += ", " + pluralize(pvr.Templates, "template")
			}
			detail += ")"
			sections = append(sections, sectionResult{
				Name:   "Workflows",
				Status: "OK",
				Detail: detail,
			})
		}
	}

	// 8. Layers validation
	if m.LayersDir != "" {
		sections = append(sections, validateLayers(m.LayersDir, g))
	}

	// 9. Plans validation
	if len(m.PlanDirs) > 0 {
		graphDir := filepath.Dir(m.GraphPath)
		pvr := validatePlans([]string(m.PlanDirs), g, graphDir, m.LayersDir)
		if len(pvr.Errors) > 0 {
			sections = append(sections, sectionResult{
				Name:   "Plans",
				Status: "FAILED",
				Errors: pvr.Errors,
			})
		} else if pvr.Total > 0 {
			detail := "(" + pluralize(pvr.Total, "file")
			if pvr.Recipes > 0 {
				detail += ", " + pluralize(pvr.Recipes, "recipe")
			}
			detail += ")"
			sections = append(sections, sectionResult{
				Name:   "Plans",
				Status: "OK",
				Detail: detail,
			})
		}
	}

	printSections(out, sections)

	// Determine overall result
	failedCount, warnCount := 0, 0
	for _, s := range sections {
		switch s.Status {
		case "FAILED":
			failedCount++
		case "WARN":
			warnCount++
		}
	}

	_, _ = fmt.Fprintln(out)
	if failedCount > 0 {
		_, _ = fmt.Fprintf(out, "Project validation: FAILED (%d section(s) with errors)\n", failedCount)
		return 1
	}
	if warnCount > 0 {
		_, _ = fmt.Fprintf(out, "Project validation: PASSED with warnings in %s (--strict fails on them)\n", pluralize(warnCount, "section"))
		return 0
	}
	_, _ = fmt.Fprintln(out, "Project validation: PASSED")
	return 0
}

// issueStatus is the status of a section that found problems: FAILED for
// errors, or for warnings under --strict; WARN for warnings otherwise.
func issueStatus(hasErrors, strict bool) string {
	if hasErrors || strict {
		return "FAILED"
	}
	return "WARN"
}

// workflowValidationResult holds counts from workflow validation for the detail string.
type workflowValidationResult struct {
	Errors    []string
	Total     int
	Templates int
}

// validateWorkflows walks the workflows directory and validates each .yaml file.
// Files referenced as workflow templates are only parsed (structural check),
// not graph-validated, since their missing inputs get wired at composition time.
func validateWorkflows(workflowsDir string, g *graph.Graph, workflowTemplates map[string]bool) workflowValidationResult {
	var result workflowValidationResult
	// Walk subdirectories too: projects keep slot options and addons in
	// workflows/slots/ and workflows/addons/.
	err := filepath.WalkDir(workflowsDir, func(planPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (!strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml")) {
			return nil
		}
		result.Total++
		p, err := plan.ParseFile(planPath)
		if err != nil {
			result.Errors = append(result.Errors, err.Error()) // names the file
			return nil
		}

		// Skip graph validation for workflow templates — they are intentionally
		// incomplete and get their missing inputs wired by intent.Compose.
		abs, err := filepath.Abs(planPath)
		if err == nil && workflowTemplates[abs] {
			result.Templates++
			return nil
		}

		if _, err := plan.InstantiateAndValidate(p, g); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", planPath, err))
		}
		return nil
	})
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("reading workflows directory: %s", err))
	}

	return result
}

// workflowTemplatePaths collects absolute file paths for all workflow templates
// referenced in the graph, resolved relative to graphDir.
func workflowTemplatePaths(g *graph.Graph, graphDir string) map[string]bool {
	paths := make(map[string]bool)
	for _, wf := range g.Workflows {
		if wf.Template == "" {
			continue
		}
		resolved := wf.Template
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(graphDir, resolved)
		}
		if abs, err := filepath.Abs(resolved); err == nil {
			paths[abs] = true
		}
	}
	return paths
}

// planValidationResult holds counts from plan validation.
type planValidationResult struct {
	Errors  []string
	Total   int
	Recipes int
}

// validatePlans walks all plan directories and validates each plan file.
// Recipe-format files are reconstituted into full plans before validation,
// with their layers loaded from layersDir.
func validatePlans(planDirs []string, g *graph.Graph, graphDir, layersDir string) planValidationResult {
	var result planValidationResult

	entries, err := config.ListPlans(planDirs)
	if err != nil {
		result.Errors = []string{fmt.Sprintf("listing plans: %s", err)}
		return result
	}

	for _, entry := range entries {
		result.Total++
		parsed, err := plan.ParseAnyFile(entry.FullPath)
		if err != nil {
			result.Errors = append(result.Errors, err.Error()) // names the file
			continue
		}
		switch v := parsed.(type) {
		case *plan.Plan:
			if _, err := plan.InstantiateAndValidate(v, g); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", entry.FullPath, err))
			}
		case *plan.Recipe:
			result.Recipes++
			if _, err := intent.Reconstitute(v, g, graphDir, intent.WithLayersDir(layersDir)); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: reconstituting recipe: %s", entry.FullPath, err))
			}
		}
	}

	return result
}

// validateLayers loads every layer in dir and reports parse errors, duplicate
// names, and input keys that match nothing in the graph. ApplyLayers ignores
// such keys, so a typo would otherwise stop a layer from taking effect silently.
func validateLayers(dir string, g *graph.Graph) sectionResult {
	layers, err := graph.LoadLayersFromDir(dir)
	if err != nil {
		return sectionResult{Name: "Layers", Status: "FAILED", Errors: []string{err.Error()}}
	}

	names := make([]string, 0, len(layers))
	for name := range layers {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []string
	for _, name := range names {
		for _, key := range layers[name].UnknownInputs(g) {
			errs = append(errs, fmt.Sprintf("layer %q: input %q matches no node input in the graph", name, key))
		}
	}
	if len(errs) > 0 {
		return sectionResult{Name: "Layers", Status: "FAILED", Errors: errs}
	}
	return sectionResult{Name: "Layers", Status: "OK", Detail: "(" + pluralize(len(layers), "layer") + ")"}
}

// validateDomain parses the domain knowledge file.
func validateDomain(path string) sectionResult {
	kb, err := domain.ParseFile(path)
	if err != nil {
		return sectionResult{Name: "Domain", Status: "FAILED", Errors: []string{err.Error()}}
	}
	return sectionResult{Name: "Domain", Status: "OK", Detail: fmt.Sprintf("(%s, %s, %s)",
		pluralize(len(kb.Concepts), "concept"), pluralize(len(kb.Types), "type"), pluralize(len(kb.ValuePools), "value pool"))}
}

// validateVisualizers loads the visualizer manifest and checks that each
// visualizer's HTML file exists.
func validateVisualizers(dir string) sectionResult {
	defs, err := config.LoadVisualizers(dir)
	if err != nil {
		return sectionResult{Name: "Visualizers", Status: "FAILED", Errors: []string{err.Error()}}
	}
	return sectionResult{Name: "Visualizers", Status: "OK", Detail: "(" + pluralize(len(defs), "visualizer") + ")"}
}

// shortenManifestPaths rewrites the manifest's paths relative to the working
// directory where that is shorter. Every error aat validate prints names its
// file, and "plans/smoke.yaml" reads better than an absolute path.
func shortenManifestPaths(m *config.ProjectManifest) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	shorten := func(path string) string {
		if path == "" || !filepath.IsAbs(path) {
			return path
		}
		if rel, err := filepath.Rel(wd, path); err == nil && len(rel) < len(path) {
			return rel
		}
		return path
	}
	for _, p := range []*string{&m.GraphPath, &m.TemplatesPath, &m.DomainPath, &m.EnvPath,
		&m.WorkflowsDir, &m.LayersDir, &m.VisualizersDir} {
		*p = shorten(*p)
	}
	for i := range m.PlanDirs {
		m.PlanDirs[i] = shorten(m.PlanDirs[i])
	}
}

// pluralize renders a count with its noun: pluralize(1, "file") is "1 file",
// pluralize(2, "file") is "2 files". Every noun aat validate counts takes "s".
func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// printSections prints the validation sections in aligned columns.
func printSections(out io.Writer, sections []sectionResult) {
	// Find max name length for alignment
	maxLen := 0
	for _, s := range sections {
		if len(s.Name) > maxLen {
			maxLen = len(s.Name)
		}
	}

	for _, s := range sections {
		padding := strings.Repeat(" ", maxLen-len(s.Name)+1)
		line := fmt.Sprintf("%s:%s%s", s.Name, padding, s.Status)
		if s.Detail != "" {
			line += " " + s.Detail
		}
		_, _ = fmt.Fprintln(out, line)

		if s.Status == "FAILED" || s.Status == "WARN" {
			printed := make(map[string]bool, len(s.Errors))
			for _, e := range s.Errors {
				// An error reported more than once, word for word, is printed once
				if printed[e] {
					continue
				}
				printed[e] = true
				// Indent each line of the error
				for _, eline := range strings.Split(e, "\n") {
					if eline != "" {
						_, _ = fmt.Fprintf(out, "  %s\n", eline)
					}
				}
			}
		}
	}
}

// validateEnvironmentFile validates the environment file. For multi-env files,
// it attempts to load each non-abstract environment to verify extends chains,
// variable substitution, and structural validity. It returns the section
// describing the outcome, and whether the failure includes a --var the file
// cannot use: a problem with the invocation rather than with the project.
func validateEnvironmentFile(envPath, defaultEnv string, vars map[string]string) (*sectionResult, bool) {
	isMulti, err := config.IsMultiEnvFile(envPath)
	if err != nil {
		return &sectionResult{
			Name:   "Environment",
			Status: "FAILED",
			Errors: []string{fmt.Sprintf("reading environment file: %s", err)},
		}, false
	}

	if !isMulti {
		// Legacy single-env file — validate it loads
		_, err := config.LoadNamedEnvironmentWithVars(envPath, "", vars)
		if err != nil {
			return &sectionResult{
				Name:   "Environment",
				Status: "FAILED",
				Errors: []string{err.Error()},
			}, errors.Is(err, config.ErrUnusableVars)
		}
		return &sectionResult{
			Name:   "Environment",
			Status: "OK",
			Detail: "(single environment)",
		}, false
	}

	// Multi-env file — validate all non-abstract environments
	names, err := config.ListEnvironments(envPath)
	if err != nil {
		return &sectionResult{
			Name:   "Environment",
			Status: "FAILED",
			Errors: []string{fmt.Sprintf("listing environments: %s", err)},
		}, false
	}

	var envErrors []string
	unusableVars := false
	for _, name := range names {
		if _, err := config.LoadNamedEnvironmentWithVars(envPath, name, vars); err != nil {
			envErrors = append(envErrors, fmt.Sprintf("%s: %s", name, err))
			unusableVars = unusableVars || errors.Is(err, config.ErrUnusableVars)
		}
	}

	// Validate defaultEnvironment if set
	if defaultEnv != "" {
		found := false
		for _, name := range names {
			if name == defaultEnv {
				found = true
				break
			}
		}
		if !found {
			envErrors = append(envErrors, fmt.Sprintf("defaultEnvironment %q is not a selectable environment", defaultEnv))
		}
	}

	if len(envErrors) > 0 {
		return &sectionResult{
			Name:   "Environment",
			Status: "FAILED",
			Errors: envErrors,
		}, unusableVars
	}

	return &sectionResult{
		Name:   "Environment",
		Status: "OK",
		Detail: fmt.Sprintf("(%s: %s)", pluralize(len(names), "environment"), strings.Join(names, ", ")),
	}, false
}
