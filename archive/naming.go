package archive

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// AutoGenPrefixRe matches auto-generated run/batch directory prefixes.
var AutoGenPrefixRe = regexp.MustCompile(`^(run|batch)-`)

// IsNamed returns true if the directory name represents a named/saved run or batch.
// A directory is named if it starts with "!" or doesn't match the auto-generated
// run-/batch- prefix pattern.
func IsNamed(dirName string) bool {
	if len(dirName) > 0 && dirName[0] == '!' {
		return true
	}
	return !AutoGenPrefixRe.MatchString(dirName)
}

// CheckDirName reports an error unless name is a single directory name that
// stays inside the directory it is joined to. Run and batch IDs, saved names,
// and trace IDs all become paths inside an archive or traces directory, so
// none may be empty, "." or "..", or contain a path separator or NUL byte.
func CheckDirName(name string) error {
	if strings.ContainsAny(name, `/\`+"\x00") {
		return fmt.Errorf("invalid name %q: it must not contain path separators", name)
	}
	if name == "" || name == "." || name == ".." || !filepath.IsLocal(name) {
		return fmt.Errorf("invalid name %q: it must be a single directory name", name)
	}
	return nil
}

// SavedName returns the directory name for a run or batch saved under name, by
// aat import --name or a rename in the web UI. A name that starts like an
// auto-generated one (run-, batch-) gets a "!" prefix, so aat run clean never
// deletes it.
func SavedName(name string) (string, error) {
	if err := CheckDirName(name); err != nil {
		return "", err
	}
	if AutoGenPrefixRe.MatchString(name) {
		return "!" + name, nil
	}
	return name, nil
}
