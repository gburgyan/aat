package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write serializes an Archive as indented JSON and writes it to the given path.
// Parent directories are created if they don't exist. A lightweight summary.json
// is also written alongside the archive for fast listing. Summary write failures
// are silently ignored since the archive itself was written successfully.
func Write(a *Archive, path string) error {
	if err := writeJSON(a, path); err != nil {
		return err
	}

	// Best-effort: write summary.json alongside the archive.
	summary := BuildRunSummary(a)
	summaryPath := filepath.Join(filepath.Dir(path), "summary.json")
	_ = WriteSummary(summary, summaryPath)

	return nil
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
