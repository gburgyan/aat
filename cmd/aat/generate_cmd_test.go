package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
)

func TestGenerateCommand_MissingOAS(t *testing.T) {
	err := generateCommand(&generateArgs{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading spec")
}

func TestGenerateCommand_InvalidPath(t *testing.T) {
	err := generateCommand(&generateArgs{
		OASPath:         "nonexistent.yaml",
		OutputGraph:     "-",
		OutputTemplates: t.TempDir(),
	})
	assert.Error(t, err)
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

	_ = w.Close()
	os.Stdout = oldStdout

	require.NoError(t, genErr)

	buf := make([]byte, 10000)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	assert.Contains(t, output, "listPets")
	assert.Contains(t, output, "version:")
}

func TestGenerateCommand_OperationFilter(t *testing.T) {
	for name, operations := range map[string][]string{
		"comma-separated": {"getPet, createPet"},
		"repeated":        {"getPet", "createPet"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			graphOut := filepath.Join(dir, "graph.yaml")
			templatesOut := filepath.Join(dir, "templates")

			require.NoError(t, generateCommand(&generateArgs{
				OASPath:         "testdata/oas/petstore.yaml",
				OutputGraph:     graphOut,
				OutputTemplates: templatesOut,
				Operations:      operations,
			}))

			g, err := graph.ParseFile(graphOut)
			require.NoError(t, err)
			assert.Len(t, g.Nodes, 2)
			assert.Contains(t, g.Nodes, "getPet")
			assert.Contains(t, g.Nodes, "createPet")
			entries, err := os.ReadDir(templatesOut)
			require.NoError(t, err)
			assert.Len(t, entries, 2)
		})
	}
}

func TestCommaSeparated(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, commaSeparated([]string{"a, b", " ", "c,"}))
	assert.Empty(t, commaSeparated(nil))
}

// TestGenerateCommand_CircularSpec generates from a spec whose schemas refer
// back to themselves. The graph on stdout must parse, with no log lines from
// the OpenAPI library mixed in.
func TestGenerateCommand_CircularSpec(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	outCh := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(r)
		outCh <- data
	}()

	genErr := generateCommand(&generateArgs{
		OASPath:     "testdata/oas/circular.yaml",
		OutputGraph: "-",
	})

	_ = w.Close()
	os.Stdout = oldStdout
	out := <-outCh
	require.NoError(t, genErr)

	assert.False(t, strings.Contains(string(out), `"level":`), "log lines on stdout:\n%s", out)
	g, err := graph.Parse(out)
	require.NoError(t, err, "stdout:\n%s", out)
	assert.Contains(t, g.Nodes, "getOrder")
	assert.Contains(t, g.Nodes, "listCategories")
}

// TestGenerateCommand_OptionalParametersRender generates templates from a spec
// with optional query parameters, headers, and body properties, and renders
// them with every combination of optional values: each request must carry
// exactly the values given, in a valid URL and a valid JSON body. Generated
// templates used to place optional placeholders unconditionally, so they could
// not run without values for all of them.
func TestGenerateCommand_OptionalParametersRender(t *testing.T) {
	dir := t.TempDir()
	graphOut := filepath.Join(dir, "graph.yaml")
	templatesOut := filepath.Join(dir, "templates")
	require.NoError(t, generateCommand(&generateArgs{
		OASPath:         "testdata/oas/optional_params.yaml",
		OutputGraph:     graphOut,
		OutputTemplates: templatesOut,
	}))

	g, err := graph.ParseFile(graphOut)
	require.NoError(t, err)
	registry := adapter.NewRegistry()
	_, err = adapter.LoadTemplates(templatesOut, registry)
	require.NoError(t, err)
	require.NoError(t, engine.ValidateTemplateInputs(g, registry), "optional placeholders must not require values")

	list, err := adapter.ParseTemplateFile(filepath.Join(templatesOut, "listWidgets.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "widgets", list.Response.Extract["widgets"].Path, "an array property is extracted by name, not @this")

	optionalQuery := map[string]any{"color": "red", "size": 3, "page": 2}
	for _, subset := range subsets([]string{"color", "size", "page", "X-Trace"}) {
		inputs := map[string]any{}
		for _, k := range subset {
			if v, ok := optionalQuery[k]; ok {
				inputs[k] = v
			} else {
				inputs[k] = "trace-1"
			}
		}
		req, err := adapter.NewTemplateAdapter(*list).BuildRequest(inputs, &adapter.EnvironmentConfig{})
		require.NoError(t, err, "subset %v", subset)
		u, err := url.Parse(req.Path)
		require.NoError(t, err, "subset %v: %s", subset, req.Path)
		assert.Equal(t, "/widgets", u.Path, "subset %v: %s", subset, req.Path)
		wantQuery := url.Values{}
		for k, v := range optionalQuery {
			if _, ok := inputs[k]; ok {
				wantQuery.Set(k, fmt.Sprint(v))
			}
		}
		assert.Equal(t, wantQuery, u.Query(), "subset %v: %s", subset, req.Path)
		_, hasTrace := req.Headers["X-Trace"]
		_, wantTrace := inputs["X-Trace"]
		assert.Equal(t, wantTrace, hasTrace, "subset %v: an unset optional header is not sent", subset)
	}

	create, err := adapter.ParseTemplateFile(filepath.Join(templatesOut, "createWidget.yaml"))
	require.NoError(t, err)
	optionalBody := map[string]any{"labels": []any{"a", "b"}, "note": "hi", "tag": "t1"}
	for _, subset := range subsets([]string{"labels", "note", "tag"}) {
		inputs := map[string]any{"name": "gizmo", "count": 2, "dryRun": true}
		for _, k := range subset {
			inputs[k] = optionalBody[k]
		}
		req, err := adapter.NewTemplateAdapter(*create).BuildRequest(inputs, &adapter.EnvironmentConfig{})
		require.NoError(t, err, "subset %v", subset)

		var body map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &body), "subset %v: body is not JSON: %s", subset, req.Body)
		assert.Equal(t, "gizmo", body["name"])
		assert.Equal(t, float64(2), body["count"], "integers are JSON numbers")
		_, hasLabels := body["labels"]
		_, wantLabels := inputs["labels"]
		assert.Equal(t, wantLabels, hasLabels, "subset %v: %s", subset, req.Body)
		if wantLabels {
			assert.Equal(t, []any{"a", "b"}, body["labels"], "arrays are JSON arrays")
		}
		_, hasNote := body["note"]
		_, wantNote := inputs["note"]
		assert.Equal(t, wantNote, hasNote, "subset %v: %s", subset, req.Body)

		u, err := url.Parse(req.Path)
		require.NoError(t, err)
		assert.Equal(t, "true", u.Query().Get("dryRun"))
		_, wantTag := inputs["tag"]
		assert.Equal(t, wantTag, u.Query().Has("tag"), "subset %v: %s", subset, req.Path)
	}
}

// TestGenerateCommand_GraphToStdoutWritesNothing: previewing the graph on
// stdout leaves the filesystem alone unless --output-templates is given.
func TestGenerateCommand_GraphToStdoutWritesNothing(t *testing.T) {
	spec, err := filepath.Abs("testdata/oas/petstore.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	t.Chdir(dir)

	require.NoError(t, generateCommand(&generateArgs{OASPath: spec, OutputGraph: "-", OutputTemplates: "templates"}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no templates directory is written for --output-graph -")

	require.NoError(t, generateCommand(&generateArgs{OASPath: spec, OutputGraph: "-", OutputTemplates: "templates", TemplatesExplicit: true}))
	_, err = os.Stat(filepath.Join(dir, "templates", "createPet.yaml"))
	assert.NoError(t, err, "an explicit --output-templates is honored")
}

// subsets returns every subset of items, the empty one included.
func subsets(items []string) [][]string {
	var out [][]string
	for mask := 0; mask < 1<<len(items); mask++ {
		var s []string
		for i, item := range items {
			if mask&(1<<i) != 0 {
				s = append(s, item)
			}
		}
		out = append(out, s)
	}
	return out
}
