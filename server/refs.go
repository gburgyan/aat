package server

import (
	"fmt"

	"github.com/gburgyan/aat/archive"
)

// checkRef reports a run, batch, or trace ID that is not a single directory
// name as notFound. IDs come from request paths and are joined into file
// paths, so an ID such as ".." would otherwise reach outside the archive or
// traces directory.
func checkRef(kind, id string, notFound error) error {
	if archive.CheckDirName(id) != nil {
		return fmt.Errorf("%s %q: %w", kind, id, notFound)
	}
	return nil
}
