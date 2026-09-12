package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDirName(t *testing.T) {
	for _, name := range []string{"run-20260101-120000-abcd1234", "!run-x", "nightly-run-3", "my run"} {
		assert.NoError(t, CheckDirName(name), name)
	}
	for _, name := range []string{"", ".", "..", "../x", "a/b", `a\b`, "/abs", "nul\x00byte"} {
		assert.Error(t, CheckDirName(name), name)
	}
}

func TestSavedName(t *testing.T) {
	got, err := SavedName("my-debug-run")
	require.NoError(t, err)
	assert.Equal(t, "my-debug-run", got)

	got, err = SavedName("run-keep")
	require.NoError(t, err)
	assert.Equal(t, "!run-keep", got)

	_, err = SavedName("../x")
	assert.Error(t, err)
}

func TestImportArchive_RejectsNameOutsideDestination(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("archive.json")
	require.NoError(t, err)
	_, _ = w.Write([]byte(`{}`))
	require.NoError(t, zw.Close())

	parent := t.TempDir()
	dest := filepath.Join(parent, "runs")
	require.NoError(t, os.MkdirAll(dest, 0o755))

	data := buf.Bytes()
	_, _, err = ImportArchive(bytes.NewReader(data), int64(len(data)), "../escaped", dest)
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(parent, "escaped"))
	assert.True(t, os.IsNotExist(statErr), "nothing is written outside the archive directory")
}
