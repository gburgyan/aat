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
		StoppedAt: "createItinerary",
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
				StepID:        "createItinerary",
				Node:          "createItinerary",
				Outputs:       map[string]any{"itineraryId": "wb-42"},
				ActualBaseURL: "https://api.example.com",
				Request: &adapter.Request{
					Headers: map[string]string{"Authorization": "Bearer live-token"},
				},
			},
		},
	}

	exp := BuildStateExport(result, "https://api.example.com")

	assert.Equal(t, "1", exp.Version)
	assert.Equal(t, "stopped", exp.Outcome)
	assert.Equal(t, "createItinerary", exp.StoppedAt)

	// Base URL and unredacted auth come from the last request to the default route.
	assert.Equal(t, "https://api.example.com", exp.BaseURL)
	assert.Equal(t, "Bearer live-token", exp.Auth.Headers["Authorization"])

	// Flattened convenience values.
	assert.Equal(t, "abc", exp.Values["login.token"])
	assert.Equal(t, "wb-42", exp.Values["createItinerary.itineraryId"])

	require.Len(t, exp.Steps, 2)
	assert.Equal(t, "alice", exp.Steps[0].Inputs["user"])
	assert.Equal(t, "https://api.example.com", exp.Steps[1].BaseURL)
	assert.Equal(t, "Bearer live-token", exp.Steps[1].Headers["Authorization"])
}

// TestBuildStateExport_PerRoute: when the last step was routed to another host
// with its own credential, the top level still describes the default route and
// each step carries the host and headers it actually used.
func TestBuildStateExport_PerRoute(t *testing.T) {
	const shop, pay = "http://shop.local/us/v1", "http://pay.local/us/v1"
	steps := []StepResult{
		{
			StepID: "checkout", Node: "checkoutCart", ActualBaseURL: shop,
			Request: &adapter.Request{Headers: map[string]string{"Authorization": "Bearer shop-token"}},
		},
		{
			StepID: "paymentCharge", Node: "paymentCharge", ActualBaseURL: pay,
			Request: &adapter.Request{Headers: map[string]string{"X-API-Key": "pay-key"}},
		},
	}

	tests := []struct {
		name           string
		defaultBaseURL string
		wantBaseURL    string
		wantHeaders    map[string]string
	}{
		{name: "default route wins", defaultBaseURL: shop, wantBaseURL: shop, wantHeaders: map[string]string{"Authorization": "Bearer shop-token"}},
		{name: "no request to the default route falls back to the last request", defaultBaseURL: "http://elsewhere", wantBaseURL: pay, wantHeaders: map[string]string{"X-API-Key": "pay-key"}},
		{name: "unknown default falls back to the last request", defaultBaseURL: "", wantBaseURL: pay, wantHeaders: map[string]string{"X-API-Key": "pay-key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := BuildStateExport(&RunResult{Outcome: OutcomeStopped, StoppedAt: "paymentCharge", Steps: steps}, tt.defaultBaseURL)

			assert.Equal(t, tt.wantBaseURL, exp.BaseURL)
			assert.Equal(t, tt.wantHeaders, exp.Auth.Headers)
			require.Len(t, exp.Steps, 2)
			assert.Equal(t, pay, exp.Steps[1].BaseURL)
			assert.Equal(t, map[string]string{"X-API-Key": "pay-key"}, exp.Steps[1].Headers)
			assert.Equal(t, map[string]string{"Authorization": "Bearer shop-token"}, exp.Steps[0].Headers)
		})
	}
}

func TestBuildStateExport_SkipsErroredSteps(t *testing.T) {
	result := &RunResult{
		Outcome: OutcomeFailed,
		Steps: []StepResult{
			{StepID: "ok", Node: "ok", Outputs: map[string]any{"id": "1"}, ActualBaseURL: "https://h", Request: &adapter.Request{Headers: map[string]string{"Authorization": "tok"}}},
			{StepID: "bad", Node: "bad", Error: assertErr{}},
		},
	}

	exp := BuildStateExport(result, "https://h")
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

// TestWriteStateExport_ExistingFileGetsMode0600: overwriting a file that was
// created with looser permissions still leaves the credentials at mode 0600.
func TestWriteStateExport_ExistingFileGetsMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	require.NoError(t, WriteStateExport(&StateExport{Version: "1", Outcome: "stopped"}, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"outcome": "stopped"`)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}

// TestRedactStateExport: a redacted export masks credential headers at the top
// level and in every step, replaces known secrets wherever they appear, keeps
// identifiers and other data, and leaves the export it copies unchanged.
func TestRedactStateExport(t *testing.T) {
	const secret = "s3cret-api-key-123"
	result := &RunResult{
		Outcome:   OutcomeStopped,
		StoppedAt: "pay",
		Steps: []StepResult{
			{
				StepID: "login", Node: "login", ActualBaseURL: "https://api.example.com",
				Inputs:  map[string]any{"apiKey": secret, "user": "alice"},
				Outputs: map[string]any{"session": "sess-" + secret, "count": 3},
				Request: &adapter.Request{Headers: map[string]string{"Authorization": "Bearer issued-token", "Accept": "application/json"}},
			},
			{
				StepID: "pay", Node: "pay", ActualBaseURL: "https://pay.example.com",
				Outputs: map[string]any{"status": "pay"},
				Request: &adapter.Request{Headers: map[string]string{"X-Custom-Key": secret}},
			},
		},
	}
	exp := BuildStateExport(result, "https://api.example.com")

	red, err := RedactStateExport(exp, map[string]bool{secret: true, "pay": true})
	require.NoError(t, err)

	assert.True(t, red.Redacted)
	assert.Equal(t, "[REDACTED]", red.Auth.Headers["Authorization"], "a credential header is masked by name")
	assert.Equal(t, "application/json", red.Auth.Headers["Accept"])
	require.Len(t, red.Steps, 2)
	assert.Equal(t, "[REDACTED]", red.Steps[0].Headers["Authorization"])
	assert.Equal(t, "[REDACTED]", red.Steps[1].Headers["X-Custom-Key"], "a known secret is masked under any header")
	assert.Equal(t, "[REDACTED]", red.Steps[0].Inputs["apiKey"])
	assert.Equal(t, "alice", red.Steps[0].Inputs["user"])
	assert.Equal(t, "sess-[REDACTED]", red.Steps[0].Outputs["session"])
	assert.Equal(t, "sess-[REDACTED]", red.Values["login.session"])
	assert.Equal(t, "https://pay.example.com", red.Steps[1].BaseURL)

	// A short secret is replaced only where a whole value equals it, and never in
	// the identifiers a harness looks steps up by.
	assert.Equal(t, "[REDACTED]", red.Steps[1].Outputs["status"])
	assert.Equal(t, "pay", red.StoppedAt)
	assert.Equal(t, "pay", red.Steps[1].StepID)
	assert.Equal(t, "pay", red.Steps[1].Node)
	assert.Contains(t, red.Values, "pay.status", "map keys are kept")

	data, err := json.Marshal(red)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"redacted":true`)
	assert.NotContains(t, string(data), secret)
	assert.NotContains(t, string(data), "issued-token")

	// The export it copied still holds what was sent.
	assert.False(t, exp.Redacted)
	assert.Equal(t, "Bearer issued-token", exp.Auth.Headers["Authorization"])
	assert.Equal(t, secret, exp.Steps[0].Inputs["apiKey"])
}

// TestRedactStateExport_NoKnownSecrets: credential headers are masked by name
// even when the run has no known secrets, such as a token issued at run time.
func TestRedactStateExport_NoKnownSecrets(t *testing.T) {
	exp := BuildStateExport(&RunResult{Outcome: OutcomePassed, Steps: []StepResult{{
		StepID: "a", Node: "a", ActualBaseURL: "https://h",
		Request: &adapter.Request{Headers: map[string]string{"Authorization": "Bearer t"}},
	}}}, "https://h")

	red, err := RedactStateExport(exp, nil)
	require.NoError(t, err)
	assert.True(t, red.Redacted)
	assert.Equal(t, "[REDACTED]", red.Auth.Headers["Authorization"])
	assert.Equal(t, "[REDACTED]", red.Steps[0].Headers["Authorization"])
	assert.Equal(t, "Bearer t", exp.Steps[0].Headers["Authorization"], "the original export is unchanged")
}
