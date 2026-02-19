package adapter

import (
	"strings"
)

// PathRewrite controls URL path rewriting for host overrides.
// This mirrors config.PathRewrite but lives in adapter to avoid a dependency cycle.
type PathRewrite struct {
	Strip  string // prefix to remove from the template path
	Prefix string // prefix to add after stripping
}

// RewritePath applies strip/prefix rewriting to a request path.
// If rewrite is nil, the path is returned unchanged.
func RewritePath(path string, rewrite *PathRewrite) string {
	if rewrite == nil {
		return path
	}

	// Strip: remove prefix if present
	if rewrite.Strip != "" {
		path = strings.TrimPrefix(path, rewrite.Strip)
	}

	// Prefix: prepend
	if rewrite.Prefix != "" {
		// Avoid double slashes
		prefix := rewrite.Prefix
		if strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, "/") {
			prefix = strings.TrimSuffix(prefix, "/")
		}
		// Ensure there's a slash between prefix and path if neither has one
		if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(path, "/") && path != "" {
			prefix = prefix + "/"
		}
		path = prefix + path
	}

	return path
}
