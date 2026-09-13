package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gburgyan/aat/archive"
)

// StateExport is a self-contained snapshot of the live state accumulated by a
// run, intended for consumption by an external test harness.
//
// SECURITY: BuildStateExport copies request headers as they were sent, auth
// tokens included, and RedactStateExport removes the credentials. aat run plan
// redacts every dump unless --dump-state-secrets asks for live credentials,
// which a harness needs to send requests as the run's session. Files written
// via WriteStateExport use mode 0600 either way. Do not commit or share them.
type StateExport struct {
	Version string `json:"version" redact:"-"`
	// Redacted is true when RedactStateExport removed the credentials.
	Redacted  bool           `json:"redacted"`
	Outcome   string         `json:"outcome" redact:"-"`
	StoppedAt string         `json:"stoppedAt,omitempty" redact:"-"`
	BaseURL   string         `json:"baseUrl"`
	Auth      StateAuth      `json:"auth"`
	Steps     []StateStep    `json:"steps"`
	Values    map[string]any `json:"values"`
}

// StateAuth holds the request headers of the environment's default route,
// credential headers included.
type StateAuth struct {
	Headers map[string]string `json:"headers"`
}

// StateStep captures one executed step: where its request went, the headers it
// sent, its outputs, and its resolved inputs.
type StateStep struct {
	StepID  string            `json:"stepId" redact:"-"`
	Node    string            `json:"node" redact:"-"`
	BaseURL string            `json:"baseUrl,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Outputs map[string]any    `json:"outputs,omitempty"`
	Inputs  map[string]any    `json:"inputs,omitempty"`
}

// BuildStateExport assembles a StateExport from a completed (or checkpointed)
// run. It includes every successfully executed step's outputs and resolved
// inputs, flattens outputs into a "stepID.outputName" → value convenience map,
// and records each step's base URL and request headers as they were sent. Pass
// the export through RedactStateExport unless live credentials are wanted.
//
// The top-level baseUrl and auth describe the environment's default route: they
// come from the last request sent to defaultBaseURL, so a plan whose last step
// was routed to another host (such as a payments API with its own API key)
// still exports the main session. When no request went to defaultBaseURL, they
// come from the last request issued.
func BuildStateExport(result *RunResult, defaultBaseURL string) *StateExport {
	exp := &StateExport{
		Version:   "1",
		Outcome:   result.Outcome.String(),
		StoppedAt: result.StoppedAt,
		Auth:      StateAuth{Headers: map[string]string{}},
		Steps:     []StateStep{},
		Values:    map[string]any{},
	}

	var last, lastDefault *StateStep
	for _, s := range result.Steps {
		if s.Error != nil {
			continue
		}
		step := StateStep{
			StepID:  s.StepID,
			Node:    s.Node,
			Outputs: s.Outputs,
			Inputs:  s.Inputs,
		}
		if s.Request != nil {
			step.BaseURL = s.ActualBaseURL
			step.Headers = make(map[string]string, len(s.Request.Headers))
			for k, v := range s.Request.Headers {
				step.Headers[k] = v
			}
		}
		exp.Steps = append(exp.Steps, step)
		for name, val := range s.Outputs {
			exp.Values[s.StepID+"."+name] = val
		}
	}

	for i := range exp.Steps {
		if exp.Steps[i].Headers == nil {
			continue
		}
		last = &exp.Steps[i]
		if defaultBaseURL != "" && exp.Steps[i].BaseURL == defaultBaseURL {
			lastDefault = &exp.Steps[i]
		}
	}
	session := lastDefault
	if session == nil {
		session = last
	}
	if session != nil {
		exp.BaseURL = session.BaseURL
		for k, v := range session.Headers {
			exp.Auth.Headers[k] = v
		}
	}

	return exp
}

// RedactStateExport returns a copy of exp with its credentials removed the way
// run archives remove them: the values of credential headers such as
// Authorization and X-API-Key, at the top level and in every step, and every
// known secret wherever it appears, inputs, outputs, and values included. The
// copy has Redacted set, and exp is not modified. Step IDs, node names, the
// outcome, and map keys are identifiers a harness looks things up by, so they
// are left alone.
func RedactStateExport(exp *StateExport, secrets map[string]bool) (*StateExport, error) {
	cp := *exp
	cp.Redacted = true
	cp.Auth = StateAuth{Headers: archive.RedactHeaders(exp.Auth.Headers)}
	cp.Steps = make([]StateStep, len(exp.Steps))
	for i, step := range exp.Steps {
		step.Headers = archive.RedactHeaders(step.Headers)
		cp.Steps[i] = step
	}
	return archive.Redact(&cp, secrets)
}

// WriteStateExport writes the export as indented JSON to path with mode 0600.
// The restrictive mode reflects that a dump with live credentials holds them in
// plain text. The file is written to a temporary file in the same directory and
// renamed into place, so an existing file at path also ends up with mode 0600.
func WriteStateExport(exp *StateExport, path string) error {
	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state export: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating state export dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("writing state export: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing state export: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing state export: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing state export: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing state export: %w", err)
	}
	return nil
}
