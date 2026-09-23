package yamlx

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type testDoc struct {
	Name       string               `yaml:"name"`
	Steps      []testStep           `yaml:"steps"`
	Values     map[string]testValue `yaml:"values"`
	Nested     *testNested          `yaml:"nested"`
	Ignored    string               `yaml:"-"`
	Default    string
	testInline `yaml:",inline"`
}

type testInline struct {
	Extra string `yaml:"extra"`
}

type testNested struct {
	Optional bool `yaml:"optional"`
}

type testStep struct {
	ID    string    `yaml:"id"`
	Value testValue `yaml:"value"`
}

// testValue mirrors plan.StepValue: a scalar shorthand or a mapping, decoded
// with the callback form so strictness reaches the mapping.
type testValue struct {
	Default any    `yaml:"default"`
	From    string `yaml:"from"`
}

func (v *testValue) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return unmarshal(&v.Default)
	case yaml.MappingNode:
		type rawTestValue testValue
		var raw rawTestValue
		if err := unmarshal(&raw); err != nil {
			return err
		}
		*v = testValue(raw)
		return nil
	}
	return KindError(n, "a test value", "a scalar or a mapping")
}

// nodeValue decodes a mapping with the node form; strictness does not reach it.
type nodeValue struct {
	From string `yaml:"from"`
}

func (v *nodeValue) UnmarshalYAML(n *yaml.Node) error {
	type rawNodeValue nodeValue
	var raw rawNodeValue
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*v = nodeValue(raw)
	return nil
}

func decodeIssues(t *testing.T, src string, v any) []Issue {
	t.Helper()
	err := Decode([]byte(src), v)
	if err == nil {
		return nil
	}
	var yerr *Error
	require.True(t, errors.As(err, &yerr), "error type %T: %v", err, err)
	return yerr.Issues
}

func TestDecode_UnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Issue
	}{
		{
			name: "top level, close match",
			src:  "name: a\nstepz: []\n",
			want: []Issue{{Line: 2, Message: `unknown key "stepz" in test doc (did you mean "steps"?)`}},
		},
		{
			name: "top level, nothing close",
			src:  "name: a\nbogus: 1\n",
			want: []Issue{{Line: 2, Message: `unknown key "bogus" in test doc (valid keys: default, extra, name, nested, steps, values)`}},
		},
		{
			name: "inside a list element",
			src:  "steps:\n  - id: s1\n    idd: x\n",
			want: []Issue{{Line: 3, Message: `unknown key "idd" in test step (did you mean "id"?)`}},
		},
		{
			name: "inside a pointer struct",
			src:  "nested:\n  Optional: true\n",
			want: []Issue{{Line: 2, Message: `unknown key "Optional" in test nested (did you mean "optional"?)`}},
		},
		{
			name: "inside a callback unmarshaler's mapping",
			src:  "values:\n  a: {fromm: x.y}\n",
			want: []Issue{{Line: 2, Message: `unknown key "fromm" in test value (did you mean "from"?)`}},
		},
		{
			name: "several issues in document order",
			src:  "bogus: 1\nsteps:\n  - idd: x\n",
			want: []Issue{
				{Line: 1, Message: `unknown key "bogus" in test doc (valid keys: default, extra, name, nested, steps, values)`},
				{Line: 3, Message: `unknown key "idd" in test step (did you mean "id"?)`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc testDoc
			assert.Equal(t, tt.want, decodeIssues(t, tt.src, &doc))
		})
	}
}

func TestDecode_ValidShapes(t *testing.T) {
	src := `
name: a
default: d
extra: e
steps:
  - id: s1
    value: plain
  - id: s2
    value: {from: s1.out}
values:
  scalar: 42
  mapping: {default: x}
`
	var doc testDoc
	require.NoError(t, Decode([]byte(src), &doc))
	assert.Equal(t, "d", doc.Default, "untagged fields use the lowercased name")
	assert.Equal(t, "e", doc.Extra, "inline fields are accepted")
	assert.Equal(t, "plain", doc.Steps[0].Value.Default)
	assert.Equal(t, "s1.out", doc.Steps[1].Value.From)
	assert.Equal(t, 42, doc.Values["scalar"].Default)
	assert.Equal(t, "x", doc.Values["mapping"].Default)
}

func TestDecode_NodeFormStaysLenient(t *testing.T) {
	// The reason the package insists on the callback form: a *yaml.Node
	// unmarshaler decodes with a fresh decoder that ignores unknown keys.
	var doc struct {
		Value nodeValue `yaml:"value"`
	}
	require.NoError(t, Decode([]byte("value: {from: a.b, bogus: 1}\n"), &doc))
	assert.Equal(t, "a.b", doc.Value.From)
}

func TestDecode_AnchorsAndMerges(t *testing.T) {
	src := `
nested: &opts
  optional: true
steps:
  - &base
    id: s1
  - <<: *base
    id: s2
`
	var doc testDoc
	require.NoError(t, Decode([]byte(src), &doc))
	assert.Equal(t, "s2", doc.Steps[1].ID)
	assert.True(t, doc.Nested.Optional)

	// An unknown key inside a merged anchor is reported at the anchor's line.
	bad := "steps:\n  - &base\n    idd: x\n  - <<: *base\n"
	issues := decodeIssues(t, bad, &testDoc{})
	require.NotEmpty(t, issues)
	assert.Equal(t, 3, issues[0].Line)
	assert.Contains(t, issues[0].Message, `unknown key "idd"`)
}

func TestDecode_OtherErrors(t *testing.T) {
	t.Run("duplicate key", func(t *testing.T) {
		issues := decodeIssues(t, "name: a\nname: b\n", &testDoc{})
		require.Len(t, issues, 1)
		assert.Equal(t, `duplicate key "name"`, issues[0].Message)
	})

	t.Run("list where a mapping belongs", func(t *testing.T) {
		issues := decodeIssues(t, "values: [a, b]\n", &testDoc{})
		require.Len(t, issues, 1)
		assert.Equal(t, Issue{Line: 1, Message: "expected a mapping, found a list"}, issues[0])
	})

	t.Run("wrong shape through KindError", func(t *testing.T) {
		issues := decodeIssues(t, "values:\n  a: [1, 2]\n", &testDoc{})
		require.Len(t, issues, 1)
		assert.Equal(t, Issue{Line: 2, Message: "a test value must be a scalar or a mapping, found a list"}, issues[0])
	})

	t.Run("syntax error", func(t *testing.T) {
		err := Decode([]byte("name: a\nsteps: [unclosed\n"), &testDoc{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "yaml: ")
		assert.Regexp(t, `^line \d+: invalid YAML: `, err.Error())
	})

	t.Run("empty and comment-only documents", func(t *testing.T) {
		doc := testDoc{Name: "kept"}
		require.NoError(t, Decode(nil, &doc))
		require.NoError(t, Decode([]byte("# nothing here\n"), &doc))
		assert.Equal(t, "kept", doc.Name)
	})

	t.Run("message lists several issues", func(t *testing.T) {
		err := Decode([]byte("bogus: 1\nsteps:\n  - idd: x\n"), &testDoc{})
		assert.EqualError(t, err, `2 problems:
  line 1: unknown key "bogus" in test doc (valid keys: default, extra, name, nested, steps, values)
  line 3: unknown key "idd" in test step (did you mean "id"?)`)
	})

	t.Run("wrapping keeps the error type", func(t *testing.T) {
		err := fmt.Errorf("plans/smoke.yaml: %w", Decode([]byte("bogus: 1\n"), &testDoc{}))
		var yerr *Error
		require.True(t, errors.As(err, &yerr))
		assert.Equal(t, 1, yerr.Issues[0].Line)
	})
}

func TestNoun(t *testing.T) {
	tests := map[string]string{
		"plan.rawStepValue":         "step value",
		"config.EnvironmentPartial": "environment",
		"graph.SlotDef":             "slot",
		"domain.TypeDef":            "type",
		"graph.OASRef":              "oas ref",
		"plan.Plan":                 "plan",
		"struct { Kind string }":    "mapping",
	}
	for in, want := range tests {
		assert.Equal(t, want, noun(in), in)
	}
}

func TestClosest(t *testing.T) {
	valid := []string{"fromSelection", "optional", "plans", "values"}
	assert.Equal(t, "fromSelection", Closest("fromSelecton", valid))
	assert.Equal(t, "optional", Closest("optinal", valid))
	assert.Equal(t, "plans", Closest("plan", valid))
	assert.Equal(t, "values", Closest("VALUES", valid))
	assert.Equal(t, "", Closest("unrelated", valid))
}

// TestDecode_Documents checks that a second YAML document, which yaml.v3 would
// silently ignore, is an error naming where it starts, while the document
// markers a single-document file may carry are fine.
func TestDecode_Documents(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "one document", yaml: "name: a\n"},
		{name: "leading marker", yaml: "---\nname: a\n"},
		{name: "trailing marker", yaml: "name: a\n---\n"},
		{name: "trailing marker and comment", yaml: "name: a\n---\n# nothing else\n"},
		{name: "document end marker", yaml: "name: a\n...\n"},
		{name: "two documents", yaml: "name: a\n---\nname: b\n", wantErr: "line 2: a second YAML document starts here; a project file holds exactly one"},
		{name: "second document after an empty one", yaml: "name: a\n---\n---\nname: b\n", wantErr: "line 3: a second YAML document starts here; a project file holds exactly one"},
		{name: "syntax error in a second document", yaml: "name: a\n---\nname: [b\n", wantErr: "invalid YAML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc testDoc
			err := Decode([]byte(tt.yaml), &doc)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, "a", doc.Name)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
