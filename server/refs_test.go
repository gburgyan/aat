package server

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArchiveIDsStayInsideArchiveDir checks that an ID from a request path
// cannot reach files outside the archive and traces directories, and that a
// rename moves only runs and batches.
func TestArchiveIDsStayInsideArchiveDir(t *testing.T) {
	parent := t.TempDir()
	archiveDir := filepath.Join(parent, "runs")
	require.NoError(t, os.MkdirAll(filepath.Join(archiveDir, "notes"), 0o755))
	// Files that ".." would reach in the parent directory.
	for name, content := range map[string]string{
		"archive.json":    `{"metadata":{"runId":"run-outside"},"steps":[],"result":{"outcome":"passed"}}`,
		"batch.json":      `{"metadata":{"batchId":"batch-outside"}}`,
		"plan-trace.json": `{}`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(parent, name), []byte(content), 0o644))
	}

	svc := NewArchiveService(archiveDir)
	for _, id := range []string{"..", ".", "../runs"} {
		_, err := svc.GetRun(id)
		assert.True(t, errors.Is(err, ErrRunNotFound), "GetRun(%q): %v", id, err)
		_, err = svc.GetBatch(id)
		assert.True(t, errors.Is(err, ErrBatchNotFound), "GetBatch(%q): %v", id, err)
		_, err = svc.ExportRun(id, io.Discard)
		assert.Error(t, err, "ExportRun(%q)", id)
		_, err = svc.ExportBatch(id, io.Discard)
		assert.Error(t, err, "ExportBatch(%q)", id)
		_, err = svc.RenameRun(id, "moved")
		assert.Error(t, err, "RenameRun(%q)", id)
		_, err = svc.UnnameBatch(id)
		assert.Error(t, err, "UnnameBatch(%q)", id)
	}

	// A directory that is neither a run nor a batch is not renamed.
	_, err := svc.RenameRun("notes", "moved")
	assert.True(t, errors.Is(err, ErrRunNotFound), "%v", err)
	assert.DirExists(t, filepath.Join(archiveDir, "notes"))

	traces := NewTraceService(archiveDir)
	_, err = traces.GetTrace("..")
	assert.True(t, errors.Is(err, ErrTraceNotFound), "%v", err)
}
