package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/validate"
)

// shownPartRow is one step's part in a listing across a run's steps.
type shownPartRow struct {
	StepID  string          `json:"step_id"`
	Node    string          `json:"node"`
	Cleanup bool            `json:"cleanup,omitempty"`
	Value   json.RawMessage `json:"value"`
}

// showStepParts prints one part of every step that has it, main steps and
// verification steps first and then cleanup steps, narrowed by --path: one line
// per step, or a JSON array with --json or --compact.
func showStepParts(out, errOut io.Writer, a *archive.Archive, opts showOptions) error {
	rows := shownPartRows(a, opts)
	if len(rows) == 0 {
		if opts.Path != "" {
			return fmt.Errorf("--path %s matches nothing in the %s of any step", opts.Path, stepPartName(opts.Part))
		}
		return fmt.Errorf("no step has %s", stepPartPhrase(opts.Part))
	}

	if opts.JSON || opts.Compact {
		var text []byte
		var err error
		if opts.Compact {
			text, err = json.Marshal(rows)
		} else {
			text, err = json.MarshalIndent(rows, "", "  ")
		}
		if err != nil {
			return err
		}
		return writeCapped(out, errOut, append(text, '\n'), opts.MaxBytes)
	}

	nodes := make([]string, len(rows))
	idWidth, nodeWidth := len("STEP"), len("NODE")
	for i, r := range rows {
		nodes[i] = r.Node
		if r.Cleanup {
			nodes[i] += " (cleanup)"
		}
		idWidth = max(idWidth, len(r.StepID))
		nodeWidth = max(nodeWidth, len(nodes[i]))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  VALUE\n", idWidth, "STEP", nodeWidth, "NODE")
	for i, r := range rows {
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n", idWidth, r.StepID, nodeWidth, nodes[i], showShort(string(r.Value), 120))
	}
	return writeCapped(out, errOut, []byte(b.String()), opts.MaxBytes)
}

// shownPartRows collects the part opts names, narrowed by its path, from each
// step that has it. A value that isn't JSON, such as a form body, is a string.
func shownPartRows(a *archive.Archive, opts showOptions) []shownPartRow {
	var rows []shownPartRow
	add := func(step *archive.StepRecord, id string, cleanup bool) {
		doc, err := stepPartJSON(step, opts.Part)
		if err != nil || len(doc) == 0 {
			return
		}
		if opts.Path != "" {
			result := gjson.GetBytes(doc, validate.NormalizeJSONPath(opts.Path))
			if !result.Exists() {
				return
			}
			doc = []byte(result.Raw)
		}
		var buf bytes.Buffer
		if json.Compact(&buf, doc) == nil {
			doc = buf.Bytes()
		} else if quoted, err := json.Marshal(string(doc)); err == nil {
			doc = quoted
		}
		rows = append(rows, shownPartRow{StepID: id, Node: step.Node, Cleanup: cleanup, Value: doc})
	}
	for i := range a.Steps {
		add(&a.Steps[i], archive.StepID(a.Steps[i]), false)
	}
	for i, id := range archive.CleanupStepIDs(a.Cleanup) {
		add(&a.Cleanup[i], id, true)
	}
	return rows
}

// stepPartPhrase names a part in a sentence: "a request body", or "outputs".
func stepPartPhrase(part string) string {
	switch part {
	case "request", "response":
		return "a " + stepPartName(part)
	}
	return part
}
