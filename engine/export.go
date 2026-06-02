package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StateExport is a self-contained snapshot of the live state accumulated by a
// run, intended for consumption by an external test harness.
//
// SECURITY: Auth headers are deliberately NOT redacted — the whole point is to
// let an external harness replay calls against the same live session. Files
// written via WriteStateExport use mode 0600 for this reason. Do not commit or
// share these files.
type StateExport struct {
	Version   string         `json:"version"`
	Outcome   string         `json:"outcome"`
	StoppedAt string         `json:"stoppedAt,omitempty"`
	BaseURL   string         `json:"baseUrl"`
	Auth      StateAuth      `json:"auth"`
	Steps     []StateStep    `json:"steps"`
	Values    map[string]any `json:"values"`
}

// StateAuth holds the live request headers (including auth tokens) from the last
// executed step, UNREDACTED.
type StateAuth struct {
	Headers map[string]string `json:"headers"`
}

// StateStep captures the outputs and resolved inputs of one executed step.
type StateStep struct {
	StepID  string         `json:"stepId"`
	Node    string         `json:"node"`
	Outputs map[string]any `json:"outputs,omitempty"`
	Inputs  map[string]any `json:"inputs,omitempty"`
}

// BuildStateExport assembles a StateExport from a completed (or checkpointed)
// run. It includes every successfully executed step's outputs and resolved
// inputs, flattens outputs into a "stepID.outputName" → value convenience map,
// and captures the base URL and live (unredacted) headers from the last step
// that issued a request.
func BuildStateExport(result *RunResult) *StateExport {
	exp := &StateExport{
		Version:   "1",
		Outcome:   result.Outcome.String(),
		StoppedAt: result.StoppedAt,
		Auth:      StateAuth{Headers: map[string]string{}},
		Steps:     []StateStep{},
		Values:    map[string]any{},
	}

	for _, s := range result.Steps {
		if s.Error != nil {
			continue
		}
		exp.Steps = append(exp.Steps, StateStep{
			StepID:  s.StepID,
			Node:    s.Node,
			Outputs: s.Outputs,
			Inputs:  s.Inputs,
		})
		for name, val := range s.Outputs {
			exp.Values[s.StepID+"."+name] = val
		}

		// Track the latest request-issuing step as the live session source.
		if s.Request != nil {
			exp.BaseURL = s.ActualBaseURL
			headers := make(map[string]string, len(s.Request.Headers))
			for k, v := range s.Request.Headers {
				headers[k] = v
			}
			exp.Auth.Headers = headers
		}
	}

	return exp
}

// WriteStateExport writes the export as indented JSON to path with mode 0600.
// The restrictive mode reflects that the file contains plaintext credentials.
func WriteStateExport(exp *StateExport, path string) error {
	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state export: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating state export dir: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing state export: %w", err)
	}
	return nil
}
