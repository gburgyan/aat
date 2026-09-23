package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/plan"
)

// fuzzSaveOptions says which fuzz cases --fuzz-save writes out, and what a
// saved plan should say about the run it came from.
type fuzzSaveOptions struct {
	Dir string
	// All also saves the cases whose finding was only a warning.
	All bool
	// PlanPath is the plan or recipe the run started from; its name starts
	// each file's, unless PlanName is set.
	PlanPath string
	// PlanName, when set, starts each file's name instead: a batch's path of
	// the plan within its directory, as in "us/smoke".
	PlanName string
	// Layers and Seed describe the run, for the saved plan's description: a
	// plain plan can't apply layers itself.
	Layers []string
	Seed   uint64
}

// saveFuzzFindings writes a plan for each fuzz case of the run with a finding
// to save. Each is the plan that ran (a recipe reconstituted), with the case
// pinned in its target step's fuzz: block and every other fuzz: block
// removed, and fail set so the finding fails it: it fails until the API is
// fixed. A case that was not sent says nothing about the API and is never
// saved. It returns the paths it wrote.
func saveFuzzFindings(p *plan.Plan, result *engine.RunResult, opts fuzzSaveOptions) ([]string, error) {
	var paths []string
	for _, s := range result.Steps {
		f := s.Fuzz
		if f == nil || f.Finding == "" || f.Finding == engine.FindingNotSent || (!f.Fails && !opts.All) {
			continue
		}
		saved, err := pinnedPlan(p, f, opts)
		if err != nil {
			return paths, fmt.Errorf("fuzz case %s: %w", f.Case.ID, err)
		}
		planName := opts.PlanName
		if planName == "" {
			planName = strings.TrimSuffix(filepath.Base(opts.PlanPath), filepath.Ext(opts.PlanPath))
		}
		name := fileSafe(planName)
		if len(opts.Layers) > 0 {
			name += "--" + fileSafe(strings.Join(opts.Layers, "+"))
		}
		name += "--" + fileSafe(f.Case.Target) + "--" + fileSafe(f.Case.ID) + ".yaml"
		path := filepath.Join(opts.Dir, name)
		if err := plan.WriteFile(saved, path); err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// pinnedPlan returns a copy of p with case f pinned on its target step, and
// a description that says where it came from.
func pinnedPlan(p *plan.Plan, f *engine.FuzzResult, opts fuzzSaveOptions) (*plan.Plan, error) {
	cp, err := plan.PinFuzzCase(p, f.Case, f.Finding, engine.DefaultFuzzFail)
	if err != nil {
		return nil, err
	}

	desc := fmt.Sprintf("Fuzz regression: case %s on step %s found %s.", f.Case.ID, f.Case.Target, f.Finding)
	desc += " It fails until the API handles the case."
	if len(opts.Layers) > 0 {
		desc += fmt.Sprintf(" Found with layers %s: run it with %s.", strings.Join(opts.Layers, ", "),
			"--layer "+strings.Join(opts.Layers, " --layer "))
	}
	if opts.Seed != 0 {
		desc += fmt.Sprintf(" Run seed: %d.", opts.Seed)
	}
	if cp.Intent.Description != "" {
		desc += " " + cp.Intent.Description
	}
	cp.Intent.Description = desc
	return cp, nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)

// fileSafe makes s usable in a file name.
func fileSafe(s string) string {
	return strings.Trim(unsafeFileChars.ReplaceAllString(s, "-"), "-")
}
