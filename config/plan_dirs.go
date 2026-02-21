package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PlanEntry describes a plan file found in a plan directory.
type PlanEntry struct {
	Name     string // relative path within the plan dir, e.g. "booking/one-way.yaml"
	FullPath string // absolute path to the file
	Dir      string // which plan dir it came from
}

// ResolvePlanWritePath resolves a plan name to a write path using the first
// entry in planDirs. Appends .yaml if the name has no recognized extension.
// Supports subdirectory names like "booking/one-way".
func ResolvePlanWritePath(planDirs []string, name string) (string, error) {
	if len(planDirs) == 0 {
		return "", fmt.Errorf("no plan directories configured")
	}

	ext := filepath.Ext(name)
	if ext != ".yaml" && ext != ".yml" {
		name = name + ".yaml"
	}

	return filepath.Join(planDirs[0], name), nil
}

// FindPlan searches all plan directories for a plan by name.
// The first match wins. Tries the name as-is, then with .yaml, then .yml appended.
// Absolute paths are checked directly without searching plan directories.
func FindPlan(planDirs []string, name string) (string, error) {
	// Absolute paths bypass directory search
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
		// Try adding extensions
		for _, ext := range []string{".yaml", ".yml"} {
			candidate := name + ext
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("plan %q not found", name)
	}

	if len(planDirs) == 0 {
		return "", fmt.Errorf("no plan directories configured")
	}

	for _, dir := range planDirs {
		// Try exact name
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Try with extensions
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			for _, tryExt := range []string{".yaml", ".yml"} {
				candidate := filepath.Join(dir, name+tryExt)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf("plan %q not found in plan directories", name)
}

// ListPlans recursively walks all plan directories and returns a PlanEntry
// for each YAML file found. Entries are deduplicated by relative name
// (first directory wins) and sorted by name.
func ListPlans(planDirs []string) ([]PlanEntry, error) {
	seen := make(map[string]bool)
	var entries []PlanEntry

	for _, dir := range planDirs {
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // skip missing dirs
			}
			return nil, fmt.Errorf("reading plan directory %s: %w", dir, err)
		}
		if !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}

			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}

			if !seen[rel] {
				seen[rel] = true
				entries = append(entries, PlanEntry{
					Name:     rel,
					FullPath: path,
					Dir:      dir,
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking plan directory %s: %w", dir, err)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
