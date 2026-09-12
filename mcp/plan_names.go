package mcp

import (
	"fmt"
	"path/filepath"
)

// checkPlanName rejects a plan name that would reach outside the plans
// directory: an absolute path, or one that climbs out with "..". Names in
// subdirectories, such as negative/state-machine, are fine.
func checkPlanName(name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("invalid plan name %q: use a name relative to the plans directory", name)
	}
	return nil
}
