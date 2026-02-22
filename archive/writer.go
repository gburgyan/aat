package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write serializes an Archive as indented JSON and writes it to the given path.
// Parent directories are created if they don't exist.
func Write(a *Archive, path string) error {
	return writeJSON(a, path)
}

// WriteBatch serializes a BatchArchive as indented JSON to the given path.
// Parent directories are created if they don't exist.
func WriteBatch(b *BatchArchive, path string) error {
	return writeJSON(b, path)
}

func writeJSON(v any, path string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling archive: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating archive directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing archive file: %w", err)
	}

	return nil
}
