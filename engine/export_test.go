package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStateExport(t *testing.T) {
	result := &RunResult{
		Outcome:   OutcomeStopped,
		Stopped:   true,
		StoppedAt: "createWorkbench",
		Steps: []StepResult{
			{
				StepID:        "login",
				Node:          "login",
				Outputs:       map[string]any{"token": "abc"},
				Inputs:        map[string]any{"user": "alice"},
				ActualBaseURL: "https://api.example.com",
				Request: &adapter.Request{
					Headers: map[string]string{"Authorization": "Bearer live-token", "Content-Type": "application/json"},
				},
			},
			{
				StepID:        "createWorkbench",
				Node:          "createWorkbench",
				Outputs:       map[string]any{"workbenchId": "wb-42"},
				ActualBaseURL: "https://api.example.com",
				Request: &adapter.Request{
					Headers: map[string]string{"Authorization": "Bearer live-token"},
				},
			},
		},
	}

	exp := BuildStateExport(result)

	assert.Equal(t, "1", exp.Version)
	assert.Equal(t, "stopped", exp.Outcome)
	assert.Equal(t, "createWorkbench", exp.StoppedAt)

	// Base URL and unredacted auth come from the last request-issuing step.
	assert.Equal(t, "https://api.example.com", exp.BaseURL)
	assert.Equal(t, "Bearer live-token", exp.Auth.Headers["Authorization"])

	// Flattened convenience values.
	assert.Equal(t, "abc", exp.Values["login.token"])
	assert.Equal(t, "wb-42", exp.Values["createWorkbench.workbenchId"])

	require.Len(t, exp.Steps, 2)
	assert.Equal(t, "alice", exp.Steps[0].Inputs["user"])
}

func TestBuildStateExport_SkipsErroredSteps(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeFailed,
		Steps: []StepResult{
			{StepID: "ok", Node: "ok", Outputs: map[string]any{"id": "1"}, ActualBaseURL: "https://h", Request: &adapter.Request{Headers: map[string]string{"Authorization": "tok"}}},
			{StepID: "bad", Node: "bad", Error: assertErr{}},
		},
	}

	exp := BuildStateExport(result)
	require.Len(t, exp.Steps, 1)
	assert.Equal(t, "ok", exp.Steps[0].StepID)
	assert.Equal(t, "tok", exp.Auth.Headers["Authorization"])
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }

func TestWriteStateExport_Mode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")

	exp := &StateExport{
		Version: "1",
		Outcome: "stopped",
		BaseURL: "https://api.example.com",
		Auth:    StateAuth{Headers: map[string]string{"Authorization": "Bearer t"}},
		Values:  map[string]any{"a.b": "c"},
	}

	require.NoError(t, WriteStateExport(exp, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var round StateExport
	require.NoError(t, json.Unmarshal(data, &round))
	assert.Equal(t, "Bearer t", round.Auth.Headers["Authorization"])
	assert.Equal(t, "c", round.Values["a.b"])
}
