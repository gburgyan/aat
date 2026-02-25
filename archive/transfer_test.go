package archive

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportImportRoundTrip_Run(t *testing.T) {
	// Create a run directory with archive.json.
	srcDir := t.TempDir()
	runDir := filepath.Join(srcDir, "test-run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), []byte(`{"metadata":{"runId":"r1"}}`), 0o644))

	// Export.
	var buf bytes.Buffer
	require.NoError(t, ExportDir(runDir, &buf))

	// Import.
	destDir := t.TempDir()
	data := buf.Bytes()
	r := bytes.NewReader(data)
	dirName, archiveType, err := ImportArchive(r, int64(len(data)), "imported-run", destDir)
	require.NoError(t, err)

	assert.Equal(t, "imported-run", dirName)
	assert.Equal(t, "run", archiveType)

	// Verify file exists.
	content, err := os.ReadFile(filepath.Join(destDir, "imported-run", "archive.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"runId":"r1"`)
}

func TestExportImportRoundTrip_RunWithAttempts(t *testing.T) {
	srcDir := t.TempDir()
	runDir := filepath.Join(srcDir, "test-run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), []byte(`{"metadata":{"runId":"r1"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "attempt-01.json"), []byte(`{"metadata":{"attempt":1}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "attempt-02.json"), []byte(`{"metadata":{"attempt":2}}`), 0o644))

	var buf bytes.Buffer
	require.NoError(t, ExportDir(runDir, &buf))

	destDir := t.TempDir()
	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, archiveType, err := ImportArchive(r, int64(len(data)), "run-with-attempts", destDir)
	require.NoError(t, err)
	assert.Equal(t, "run", archiveType)

	// Verify all files exist.
	for _, name := range []string{"archive.json", "attempt-01.json", "attempt-02.json"} {
		_, err := os.Stat(filepath.Join(destDir, "run-with-attempts", name))
		assert.NoError(t, err, "expected %s to exist", name)
	}
}

func TestExportImportRoundTrip_Batch(t *testing.T) {
	srcDir := t.TempDir()
	batchDir := filepath.Join(srcDir, "test-batch")
	require.NoError(t, os.MkdirAll(filepath.Join(batchDir, "run-001"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "batch.json"), []byte(`{"metadata":{"batchId":"b1"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "run-001", "archive.json"), []byte(`{"metadata":{"runId":"r1"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "run-001", "attempt-01.json"), []byte(`{"metadata":{"attempt":1}}`), 0o644))

	var buf bytes.Buffer
	require.NoError(t, ExportDir(batchDir, &buf))

	destDir := t.TempDir()
	data := buf.Bytes()
	r := bytes.NewReader(data)
	dirName, archiveType, err := ImportArchive(r, int64(len(data)), "imported-batch", destDir)
	require.NoError(t, err)

	assert.Equal(t, "imported-batch", dirName)
	assert.Equal(t, "batch", archiveType)

	// Verify nested structure.
	_, err = os.Stat(filepath.Join(destDir, "imported-batch", "batch.json"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(destDir, "imported-batch", "run-001", "archive.json"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(destDir, "imported-batch", "run-001", "attempt-01.json"))
	assert.NoError(t, err)
}

func TestExportDir_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "archive.json"), []byte(`{}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "image.png"), []byte("png"), 0o644))

	var buf bytes.Buffer
	require.NoError(t, ExportDir(dir, &buf))

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	assert.Len(t, zr.File, 1)
	assert.Equal(t, "archive.json", zr.File[0].Name)
}

func TestExportDir_NotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	var buf bytes.Buffer
	err := ExportDir(f, &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestImportArchive_InvalidZip(t *testing.T) {
	data := []byte("not a zip file")
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "test", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading zip")
}

func TestImportArchive_MissingMarkerFile(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("something.json")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "test", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must contain archive.json")
}

func TestImportArchive_NameCollision(t *testing.T) {
	// Create a zip with archive.json.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("archive.json")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()

	destDir := t.TempDir()
	// Pre-create the target directory.
	require.NoError(t, os.MkdirAll(filepath.Join(destDir, "existing"), 0o755))

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "existing", destDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestValidateZipContents_ZipSlip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../evil.json")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "test", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestValidateZipContents_AbsolutePath(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("/etc/passwd")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "test", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute paths")
}

func TestValidateZipContents_TooManyEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Create maxEntryCount + 1 entries.
	for i := 0; i <= maxEntryCount; i++ {
		w, _ := zw.Create(filepath.Join("dir", "file"+string(rune('a'+i%26))+".json"))
		_, _ = w.Write([]byte(`{}`))
	}
	_ = zw.Close()

	data := buf.Bytes()
	r := bytes.NewReader(data)
	_, _, err := ImportArchive(r, int64(len(data)), "test", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many entries")
}

func TestDetectArchiveType(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected string
	}{
		{"run", []string{"archive.json"}, "run"},
		{"batch", []string{"batch.json", "run-1/archive.json"}, "batch"},
		{"unknown", []string{"other.json"}, ""},
		{"run with extras", []string{"archive.json", "attempt-01.json"}, "run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, f := range tt.files {
				w, _ := zw.Create(f)
				_, _ = w.Write([]byte(`{}`))
			}
			_ = zw.Close()

			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			require.NoError(t, err)
			assert.Equal(t, tt.expected, detectArchiveType(zr))
		})
	}
}

// --- LoadRunFromZip tests ---

func TestLoadRunFromZip(t *testing.T) {
	// Create a run directory, export to zip, then load from zip.
	srcDir := t.TempDir()
	runDir := filepath.Join(srcDir, "test-run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	a := &Archive{
		Metadata: ArchiveMetadata{
			RunID:     "run-test-123",
			Timestamp: time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		},
		Steps:  []StepRecord{{Node: "TestNode", DurationMs: 100}},
		Result: ArchiveResult{Outcome: "passed"},
	}
	data, err := json.Marshal(a)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "archive.json"), data, 0o644))

	// Export to zip file.
	zipPath := filepath.Join(srcDir, "test.aar")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	require.NoError(t, ExportDir(runDir, f))
	require.NoError(t, f.Close())

	// Load from zip.
	main, attempts, err := LoadRunFromZip(zipPath)
	require.NoError(t, err)
	assert.Equal(t, "run-test-123", main.Metadata.RunID)
	assert.Equal(t, "passed", main.Result.Outcome)
	assert.Len(t, main.Steps, 1)
	assert.Empty(t, attempts)
}

func TestLoadRunFromZip_WithAttempts(t *testing.T) {
	srcDir := t.TempDir()
	runDir := filepath.Join(srcDir, "test-run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	main := &Archive{
		Metadata: ArchiveMetadata{RunID: "r1", Attempt: 3},
		Result:   ArchiveResult{Outcome: "passed"},
	}
	attempt1 := &Archive{
		Metadata: ArchiveMetadata{RunID: "r1", Attempt: 1},
		Result:   ArchiveResult{Outcome: "failed"},
	}
	attempt2 := &Archive{
		Metadata: ArchiveMetadata{RunID: "r1", Attempt: 2},
		Result:   ArchiveResult{Outcome: "failed"},
	}

	for name, a := range map[string]*Archive{
		"archive.json":    main,
		"attempt-01.json": attempt1,
		"attempt-02.json": attempt2,
	} {
		data, _ := json.Marshal(a)
		require.NoError(t, os.WriteFile(filepath.Join(runDir, name), data, 0o644))
	}

	zipPath := filepath.Join(srcDir, "test.aar")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	require.NoError(t, ExportDir(runDir, f))
	require.NoError(t, f.Close())

	loaded, attempts, err := LoadRunFromZip(zipPath)
	require.NoError(t, err)
	assert.Equal(t, "r1", loaded.Metadata.RunID)
	assert.Equal(t, 3, loaded.Metadata.Attempt)
	assert.Len(t, attempts, 2)
	assert.Equal(t, "failed", attempts[1].Result.Outcome)
	assert.Equal(t, "failed", attempts[2].Result.Outcome)
}

func TestLoadRunFromZip_MissingArchiveJSON(t *testing.T) {
	srcDir := t.TempDir()

	// Create a zip with only an attempt file, no archive.json.
	zipPath := filepath.Join(srcDir, "test.aar")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("attempt-01.json")
	_, _ = w.Write([]byte(`{"metadata":{"attempt":1}}`))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	_, _, err = LoadRunFromZip(zipPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "archive.json not found")
}

func TestLoadBatchFromZip(t *testing.T) {
	srcDir := t.TempDir()
	batchDir := filepath.Join(srcDir, "test-batch")
	require.NoError(t, os.MkdirAll(filepath.Join(batchDir, "run-001"), 0o755))

	batch := &BatchArchive{
		Metadata: BatchMetadata{BatchID: "b1", Timestamp: time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)},
		Runs:     []BatchRunEntry{{PlanName: "test-plan", RunID: "run-001", Outcome: "passed"}},
		Result:   BatchResult{Outcome: "passed", TotalRuns: 1, PassedRuns: 1},
	}
	batchData, _ := json.Marshal(batch)
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "batch.json"), batchData, 0o644))

	run := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-001"},
		Steps:    []StepRecord{{Node: "Node1", DurationMs: 50}},
		Result:   ArchiveResult{Outcome: "passed"},
	}
	runData, _ := json.Marshal(run)
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "run-001", "archive.json"), runData, 0o644))

	// Add an attempt file to the run.
	attempt := &Archive{
		Metadata: ArchiveMetadata{RunID: "run-001", Attempt: 1},
		Result:   ArchiveResult{Outcome: "failed"},
	}
	attemptData, _ := json.Marshal(attempt)
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "run-001", "attempt-01.json"), attemptData, 0o644))

	zipPath := filepath.Join(srcDir, "test.aab")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	require.NoError(t, ExportDir(batchDir, f))
	require.NoError(t, f.Close())

	loadedBatch, runs, attempts, err := LoadBatchFromZip(zipPath)
	require.NoError(t, err)
	assert.Equal(t, "b1", loadedBatch.Metadata.BatchID)
	assert.Equal(t, "passed", loadedBatch.Result.Outcome)
	assert.Len(t, runs, 1)
	assert.Equal(t, "run-001", runs["run-001"].Metadata.RunID)
	assert.Len(t, attempts, 1)
	assert.Len(t, attempts["run-001"], 1)
	assert.Equal(t, "failed", attempts["run-001"][1].Result.Outcome)
}

func TestLoadBatchFromZip_MissingBatchJSON(t *testing.T) {
	srcDir := t.TempDir()

	zipPath := filepath.Join(srcDir, "test.aab")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("run-001/archive.json")
	_, _ = w.Write([]byte(`{"metadata":{"runId":"r1"}}`))
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	_, _, _, err = LoadBatchFromZip(zipPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch.json not found")
}

func TestParseAttemptNumber(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"attempt-01.json", 1, false},
		{"attempt-02.json", 2, false},
		{"attempt-10.json", 10, false},
		{"attempt-abc.json", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseAttemptNumber(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestSanitizeArchiveName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		// Extension stripping.
		{"my-run.aar", "my-run", false},
		{"my-batch.aab", "my-batch", false},
		{"my-run.AAR", "my-run", false},
		{"my-batch.AAB", "my-batch", false},

		// Unsafe characters.
		{"hello world", "hello-world", false},
		{"run@2024!foo", "!run-2024-foo", false},
		{"a///b", "a-b", false},

		// Consecutive dashes collapsed.
		{"a---b", "a-b", false},

		// Leading/trailing dashes trimmed.
		{"-a-", "a", false},
		{"---foo---", "foo", false},

		// run-/batch- prefix gets "!" prefix.
		{"run-20240101.aar", "!run-20240101", false},
		{"batch-test.aab", "!batch-test", false},
		{"run-foo", "!run-foo", false},
		{"batch-bar", "!batch-bar", false},

		// Already clean.
		{"my-test-archive", "my-test-archive", false},
		{"test_123.aar", "test_123", false},

		// Rejections.
		{".aar", "", true},
		{".aab", "", true},
		{"...", "...", false}, // dots are safe chars; "..." is a valid name
		{"---", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := SanitizeArchiveName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
