package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
)

func TestGenerateCommand_NoNodeName(t *testing.T) {
	dir := t.TempDir()
	graphOut := filepath.Join(dir, "graph.yaml")
	require.NoError(t, generateCommand(&generateArgs{
		OASPath:         "testdata/oas/petstore.yaml",
		OutputGraph:     graphOut,
		OutputTemplates: filepath.Join(dir, "templates"),
	}))

	data, err := os.ReadFile(graphOut)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "name: listPets", "a node's name is its map key")

	g, err := graph.ParseFile(graphOut)
	require.NoError(t, err)
	assert.Equal(t, "listPets", g.Nodes["listPets"].Name)
}

func TestSpecReference(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		spec, graph, want string
	}{
		{spec: "openapi.yaml", graph: "graph.yaml", want: "openapi.yaml"},
		{spec: "specs/openapi.yaml", graph: "graph.yaml", want: "specs/openapi.yaml"},
		{spec: "openapi.yaml", graph: "project/graph.yaml", want: "../openapi.yaml"},
		{spec: "specs/openapi.yaml", graph: "-", want: "specs/openapi.yaml"},
		{spec: filepath.Join(cwd, "specs", "openapi.yaml"), graph: "project/graph.yaml", want: "../specs/openapi.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.spec+" from "+tt.graph, func(t *testing.T) {
			assert.Equal(t, tt.want, specReference(tt.spec, tt.graph))
		})
	}
}

func TestGenerateCommand_OASRelativeToGraph(t *testing.T) {
	spec, err := os.ReadFile("testdata/oas/petstore.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	specPath := filepath.Join(dir, "specs", "petstore.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(specPath), 0o755))
	require.NoError(t, os.WriteFile(specPath, spec, 0o600))
	graphOut := filepath.Join(dir, "project", "graph.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(graphOut), 0o755))

	require.NoError(t, generateCommand(&generateArgs{
		OASPath:         specPath,
		OutputGraph:     graphOut,
		OutputTemplates: filepath.Join(dir, "project", "templates"),
	}))

	g, err := graph.ParseFile(graphOut)
	require.NoError(t, err)
	assert.Equal(t, "../specs/petstore.yaml", g.OAS)
	_, err = os.Stat(filepath.Join(filepath.Dir(graphOut), g.OAS))
	assert.NoError(t, err, "the reference resolves from the graph's directory")
}

func TestGenerateCommand_RefusesOverwrite(t *testing.T) {
	tests := []struct {
		name     string
		existing []string // relative to the output directory
		force    bool
	}{
		{name: "an existing graph", existing: []string{"graph.yaml"}},
		{name: "an existing template", existing: []string{filepath.Join("templates", "createPet.yaml")}},
		{name: "--force replaces them", existing: []string{"graph.yaml", filepath.Join("templates", "createPet.yaml")}, force: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, rel := range tt.existing {
				p := filepath.Join(dir, rel)
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte("old\n"), 0o600))
			}

			err := generateCommand(&generateArgs{
				OASPath:         "testdata/oas/petstore.yaml",
				OutputGraph:     filepath.Join(dir, "graph.yaml"),
				OutputTemplates: filepath.Join(dir, "templates"),
				Force:           tt.force,
			})

			if tt.force {
				require.NoError(t, err)
				data, readErr := os.ReadFile(filepath.Join(dir, "graph.yaml"))
				require.NoError(t, readErr)
				assert.Contains(t, string(data), "listPets")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to overwrite")
			assert.Contains(t, err.Error(), "use --force")
			for _, rel := range tt.existing {
				assert.Contains(t, err.Error(), filepath.Join(dir, rel))
				data, readErr := os.ReadFile(filepath.Join(dir, rel))
				require.NoError(t, readErr)
				assert.Equal(t, "old\n", string(data), "a refused run changes nothing")
			}
			_, statErr := os.Stat(filepath.Join(dir, "templates", "listPets.yaml"))
			assert.True(t, os.IsNotExist(statErr), "and writes no other file")
		})
	}
}

func TestGenerateCommand_ObjectBodyRenders(t *testing.T) {
	dir := t.TempDir()
	graphOut := filepath.Join(dir, "graph.yaml")
	templatesOut := filepath.Join(dir, "templates")
	require.NoError(t, generateCommand(&generateArgs{
		OASPath:         "testdata/oas/object_body.yaml",
		OutputGraph:     graphOut,
		OutputTemplates: templatesOut,
	}))

	g, err := graph.ParseFile(graphOut)
	require.NoError(t, err)
	registry := adapter.NewRegistry()
	_, err = adapter.LoadTemplates(templatesOut, registry)
	require.NoError(t, err)
	require.NoError(t, engine.ValidateTemplateInputs(g, registry))

	tmpl, err := adapter.ParseTemplateFile(filepath.Join(templatesOut, "createOrder.yaml"))
	require.NoError(t, err)
	want := map[string]any{"city": "Austin", "postalCode": "78701"}
	for _, shipping := range []any{
		map[string]any{"city": "Austin", "postalCode": "78701"},
		`{"city": "Austin", "postalCode": "78701"}`,
	} {
		req, err := adapter.NewTemplateAdapter(*tmpl).BuildRequest(map[string]any{"shipping": shipping}, &adapter.EnvironmentConfig{})
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &body), "body: %s", req.Body)
		assert.Equal(t, want, body["shipping"], "a %T value is sent as a nested object", shipping)
	}
}
