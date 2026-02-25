package archive

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// maxUncompressedSize is the maximum total uncompressed size of a zip archive (500MB).
	maxUncompressedSize = 500 * 1024 * 1024
	// maxEntryCount is the maximum number of entries in a zip archive.
	maxEntryCount = 10_000
)

// safeCharsRe matches characters allowed in sanitized archive names.
var safeCharsRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// multiDashRe collapses consecutive dashes.
var multiDashRe = regexp.MustCompile(`-{2,}`)

// ExportDir walks a run or batch directory and writes all .json files
// and subdirectories as a zip archive to w. Paths in the zip are relative
// to dir.
func ExportDir(dir string, w io.Writer) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("export: stat directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("export: %s is not a directory", dir)
	}

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only include .json files.
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}
		// Use forward slashes in zip entries.
		rel = filepath.ToSlash(rel)

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening %s: %w", rel, err)
		}
		defer func() { _ = f.Close() }()

		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}

		header, err := zip.FileInfoHeader(fi)
		if err != nil {
			return fmt.Errorf("creating zip header for %s: %w", rel, err)
		}
		header.Name = rel
		header.Method = zip.Deflate

		zf, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("creating zip entry for %s: %w", rel, err)
		}
		if _, err := io.Copy(zf, f); err != nil {
			return fmt.Errorf("writing zip entry for %s: %w", rel, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("export: walking directory: %w", err)
	}

	return zw.Close()
}

// ImportArchive validates and extracts a zip archive into destDir/name.
// It returns the created directory name and the archive type ("run" or "batch").
// The archive type is determined by the presence of archive.json (run) or batch.json (batch).
func ImportArchive(r io.ReaderAt, size int64, name string, destDir string) (dirName string, archiveType string, err error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", "", fmt.Errorf("import: reading zip: %w", err)
	}

	if err := validateZipContents(zr); err != nil {
		return "", "", fmt.Errorf("import: %w", err)
	}

	// Detect archive type.
	archiveType = detectArchiveType(zr)
	if archiveType == "" {
		return "", "", fmt.Errorf("import: archive must contain archive.json (run) or batch.json (batch) at root")
	}

	// Check destination doesn't already exist.
	destPath := filepath.Join(destDir, name)
	if _, err := os.Stat(destPath); err == nil {
		return "", "", fmt.Errorf("import: directory %q already exists", name)
	}

	// Create destination directory.
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		return "", "", fmt.Errorf("import: creating directory: %w", err)
	}

	// Extract files.
	for _, f := range zr.File {
		if err := extractZipEntry(f, destPath); err != nil {
			// Clean up on failure.
			_ = os.RemoveAll(destPath)
			return "", "", fmt.Errorf("import: extracting %s: %w", f.Name, err)
		}
	}

	return name, archiveType, nil
}

// SanitizeArchiveName strips .aar/.aab extensions, replaces unsafe characters,
// and ensures the result is safe for use as a directory name.
func SanitizeArchiveName(filename string) (string, error) {
	// Strip .aar/.aab extension (case-insensitive).
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".aar") || strings.HasSuffix(lower, ".aab") {
		filename = filename[:len(filename)-4]
	}

	// Replace unsafe characters with dash.
	name := safeCharsRe.ReplaceAllString(filename, "-")

	// Collapse consecutive dashes.
	name = multiDashRe.ReplaceAllString(name, "-")

	// Trim leading/trailing dashes.
	name = strings.Trim(name, "-")

	// Reject empty, ".", ".."
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid archive name: %q", filename)
	}

	// Prefix with "!" if result matches run-/batch- pattern to avoid
	// confusion with auto-generated directory names.
	if autoGenPrefixRe.MatchString(name) {
		name = "!" + name
	}

	return name, nil
}

// autoGenPrefixRe matches auto-generated run/batch directory prefixes.
var autoGenPrefixRe = regexp.MustCompile(`^(run|batch)-`)

// validateZipContents checks zip entries for security issues.
func validateZipContents(zr *zip.Reader) error {
	if len(zr.File) > maxEntryCount {
		return fmt.Errorf("too many entries: %d (max %d)", len(zr.File), maxEntryCount)
	}

	var totalSize uint64
	for _, f := range zr.File {
		// Reject symlinks.
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed: %s", f.Name)
		}

		// Reject absolute paths.
		if filepath.IsAbs(f.Name) {
			return fmt.Errorf("absolute paths not allowed: %s", f.Name)
		}

		// Reject path traversal.
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, string(filepath.Separator)+"..") {
			return fmt.Errorf("path traversal not allowed: %s", f.Name)
		}

		// Also check each component.
		for _, part := range strings.Split(filepath.ToSlash(f.Name), "/") {
			if part == ".." {
				return fmt.Errorf("path traversal not allowed: %s", f.Name)
			}
		}

		totalSize += f.UncompressedSize64
		if totalSize > maxUncompressedSize {
			return fmt.Errorf("total uncompressed size exceeds %dMB limit", maxUncompressedSize/(1024*1024))
		}
	}

	return nil
}

// detectArchiveType returns "run" if archive.json is at root, "batch" if batch.json is at root, or "".
func detectArchiveType(zr *zip.Reader) string {
	for _, f := range zr.File {
		if f.Name == "archive.json" {
			return "run"
		}
		if f.Name == "batch.json" {
			return "batch"
		}
	}
	return ""
}

// LoadRunFromZip reads a .aar zip file and returns the main archive plus any
// attempt archives. No files are written to disk.
func LoadRunFromZip(filePath string) (main *Archive, attempts map[int]*Archive, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat archive file: %w", err)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, nil, fmt.Errorf("reading zip: %w", err)
	}

	if err := validateZipContents(zr); err != nil {
		return nil, nil, err
	}

	attempts = make(map[int]*Archive)
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		switch {
		case zf.Name == "archive.json":
			main, err = readArchiveFromZipEntry(zf)
			if err != nil {
				return nil, nil, fmt.Errorf("reading archive.json: %w", err)
			}
		case strings.HasPrefix(zf.Name, "attempt-") && strings.HasSuffix(zf.Name, ".json"):
			num, parseErr := parseAttemptNumber(zf.Name)
			if parseErr != nil {
				continue
			}
			a, readErr := readArchiveFromZipEntry(zf)
			if readErr != nil {
				return nil, nil, fmt.Errorf("reading %s: %w", zf.Name, readErr)
			}
			attempts[num] = a
		}
	}

	if main == nil {
		return nil, nil, fmt.Errorf("archive.json not found in zip")
	}

	return main, attempts, nil
}

// LoadBatchFromZip reads a .aab zip file and returns the batch archive plus
// all run archives keyed by run ID. No files are written to disk.
func LoadBatchFromZip(filePath string) (batch *BatchArchive, runs map[string]*Archive, attempts map[string]map[int]*Archive, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening batch file: %w", err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stat batch file: %w", err)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading zip: %w", err)
	}

	if err := validateZipContents(zr); err != nil {
		return nil, nil, nil, err
	}

	runs = make(map[string]*Archive)
	attempts = make(map[string]map[int]*Archive)
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := zf.Name
		if name == "batch.json" {
			batch, err = readBatchFromZipEntry(zf)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("reading batch.json: %w", err)
			}
			continue
		}

		// Expect paths like "runID/archive.json" or "runID/attempt-NN.json".
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 {
			continue
		}
		runID, fileName := parts[0], parts[1]
		if fileName == "archive.json" {
			a, readErr := readArchiveFromZipEntry(zf)
			if readErr != nil {
				return nil, nil, nil, fmt.Errorf("reading %s: %w", name, readErr)
			}
			runs[runID] = a
		} else if strings.HasPrefix(fileName, "attempt-") && strings.HasSuffix(fileName, ".json") {
			num, parseErr := parseAttemptNumber(fileName)
			if parseErr != nil {
				continue
			}
			a, readErr := readArchiveFromZipEntry(zf)
			if readErr != nil {
				return nil, nil, nil, fmt.Errorf("reading %s: %w", name, readErr)
			}
			if attempts[runID] == nil {
				attempts[runID] = make(map[int]*Archive)
			}
			attempts[runID][num] = a
		}
	}

	if batch == nil {
		return nil, nil, nil, fmt.Errorf("batch.json not found in zip")
	}

	return batch, runs, attempts, nil
}

// readArchiveFromZipEntry reads and unmarshals an Archive from a zip entry.
func readArchiveFromZipEntry(zf *zip.File) (*Archive, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// readBatchFromZipEntry reads and unmarshals a BatchArchive from a zip entry.
func readBatchFromZipEntry(zf *zip.File) (*BatchArchive, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var b BatchArchive
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// parseAttemptNumber extracts the attempt number from a filename like "attempt-01.json".
func parseAttemptNumber(filename string) (int, error) {
	// Strip directory prefix if present.
	base := filepath.Base(filename)
	base = strings.TrimPrefix(base, "attempt-")
	base = strings.TrimSuffix(base, ".json")
	return strconv.Atoi(base)
}

// extractZipEntry extracts a single zip entry to destDir.
func extractZipEntry(f *zip.File, destDir string) error {
	targetPath := filepath.Join(destDir, filepath.FromSlash(f.Name))

	// Ensure the target is within destDir (belt-and-suspenders with validateZipContents).
	rel, err := filepath.Rel(destDir, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path traversal blocked: %s", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(targetPath, 0o755)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry: %w", err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}
