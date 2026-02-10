package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMain_MissingOASFlag(t *testing.T) {
	code := generateMain([]string{})
	assert.Equal(t, 1, code)
}

func TestGenerateMain_InvalidPath(t *testing.T) {
	code := generateMain([]string{"--oas", "nonexistent.yaml"})
	assert.Equal(t, 1, code)
}

func TestGenerateCommand_Petstore(t *testing.T) {
	tmpDir := t.TempDir()
	graphOut := filepath.Join(tmpDir, "graph.yaml")
	templatesOut := filepath.Join(tmpDir, "templates")

	err := generateCommand(&generateArgs{
		OASPath:         "testdata/oas/petstore.yaml",
		OutputGraph:     graphOut,
		OutputTemplates: templatesOut,
	})
	require.NoError(t, err)

	// Verify graph file was written
	graphData, err := os.ReadFile(graphOut)
	require.NoError(t, err)
	assert.Contains(t, string(graphData), "listPets")
	assert.Contains(t, string(graphData), "createPet")
	assert.Contains(t, string(graphData), "getPet")
	assert.Contains(t, string(graphData), "deletePet")
	assert.Contains(t, string(graphData), "version:")

	// Verify template files were written
	entries, err := os.ReadDir(templatesOut)
	require.NoError(t, err)
	assert.Len(t, entries, 4)

	// Verify one template content
	tmplData, err := os.ReadFile(filepath.Join(templatesOut, "createPet.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(tmplData), "adapter: createPet")
	assert.Contains(t, string(tmplData), "method: POST")
	assert.Contains(t, string(tmplData), "Content-Type: application/json")
}

func TestGenerateCommand_Stdout(t *testing.T) {
	tmpDir := t.TempDir()
	templatesOut := filepath.Join(tmpDir, "templates")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	genErr := generateCommand(&generateArgs{
		OASPath:         "testdata/oas/petstore.yaml",
		OutputGraph:     "-",
		OutputTemplates: templatesOut,
	})

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, genErr)

	buf := make([]byte, 10000)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	assert.Contains(t, output, "listPets")
	assert.Contains(t, output, "version:")
}

func TestGenerateMain_FlagParsing(t *testing.T) {
	tmpDir := t.TempDir()
	graphOut := filepath.Join(tmpDir, "graph.yaml")
	templatesOut := filepath.Join(tmpDir, "templates")

	code := generateMain([]string{
		"--oas", "testdata/oas/petstore.yaml",
		"--output-graph", graphOut,
		"--output-templates", templatesOut,
	})
	assert.Equal(t, 0, code)

	// Verify files were created
	_, err := os.Stat(graphOut)
	assert.NoError(t, err)
	_, err = os.Stat(templatesOut)
	assert.NoError(t, err)
}
