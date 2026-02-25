package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
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
	Long:  "Validate the AAT project: manifest, graph, OAS specs, templates, and workflows.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		manifestFlag, _ := cmd.Flags().GetString("manifest")
		strict, _ := cmd.Flags().GetBool("strict")

		va := &validateArgs{
			ManifestPath: manifestFlag,
			Strict:       strict,
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
}

// validateArgs holds parsed CLI flags for the validate command.
type validateArgs struct {
	ManifestPath string
	Strict       bool
}

// sectionResult tracks the outcome of one validation section.
type sectionResult struct {
	Name   string
	Status string // "OK", "FAILED", "SKIPPED"
	Detail string // e.g. "(59 nodes)"
	Errors []string
}

// validateCommand runs full project validation. Returns 0 on success, 1 on failure.
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

	// 2. Parse graph
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
	sections = append(sections, sectionResult{
		Name:   "Graph structure",
		Status: "OK",
		Detail: fmt.Sprintf("(%d nodes)", len(g.Nodes)),
	})

	// 3. OAS validation
	validator := oas.NewValidator()
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
			if result.HasErrors() {
				sections = append(sections, sectionResult{
					Name:   "OAS validation",
					Status: "FAILED",
					Errors: []string{result.Format()},
				})
			} else if args.Strict && result.HasIssues() {
				sections = append(sections, sectionResult{
					Name:   "OAS validation",
					Status: "FAILED",
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
	registry := adapter.NewRegistry()
	n, err := adapter.LoadTemplates(m.TemplatesPath, registry)
	if err != nil {
		sections = append(sections, sectionResult{
			Name:   "Adapter outputs",
			Status: "FAILED",
			Errors: []string{err.Error()},
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
			Detail: fmt.Sprintf("(%d templates)", n),
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

	// 8. Plans validation
	if len(m.PlanDirs) > 0 {
		graphDir := filepath.Dir(m.GraphPath)
		pvr := validatePlans([]string(m.PlanDirs), g, graphDir)
		if len(pvr.Errors) > 0 {
			sections = append(sections, sectionResult{
				Name:   "Plans",
				Status: "FAILED",
				Errors: pvr.Errors,
			})
		} else if pvr.Total > 0 {
			detail := fmt.Sprintf("(%d files", pvr.Total)
			if pvr.Recipes > 0 {
				detail += fmt.Sprintf(", %d recipe", pvr.Recipes)
				if pvr.Recipes > 1 {
					detail += "s"
				}
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
	failedCount := 0
	for _, s := range sections {
		if s.Status == "FAILED" {
			failedCount++
		}
	}

	_, _ = fmt.Fprintln(out)
	if failedCount > 0 {
		_, _ = fmt.Fprintf(out, "Project validation: FAILED (%d section(s) with errors)\n", failedCount)
		return 1
	}
	_, _ = fmt.Fprintln(out, "Project validation: PASSED")
	return 0
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
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		result.Errors = []string{fmt.Sprintf("reading workflows directory: %s", err)}
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		result.Total++
		planPath := filepath.Join(workflowsDir, entry.Name())
		p, err := plan.ParseFile(planPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", entry.Name(), err))
			continue
		}

		// Skip graph validation for workflow templates — they are intentionally
		// incomplete and get their missing inputs wired by ComposeWithAddons.
		abs, err := filepath.Abs(planPath)
		if err == nil && workflowTemplates[abs] {
			result.Templates++
			continue
		}

		if _, err := plan.InstantiateAndValidate(p, g); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", entry.Name(), err))
		}
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
// Recipe-format files are reconstituted into full plans before validation.
func validatePlans(planDirs []string, g *graph.Graph, graphDir string) planValidationResult {
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
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", entry.Name, err))
			continue
		}
		switch v := parsed.(type) {
		case *plan.Plan:
			if _, err := plan.InstantiateAndValidate(v, g); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", entry.Name, err))
			}
		case *plan.Recipe:
			result.Recipes++
			if _, err := intent.Reconstitute(v, g, graphDir); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: reconstituting recipe: %s", entry.Name, err))
			}
		}
	}

	return result
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

		if s.Status == "FAILED" {
			for _, e := range s.Errors {
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
