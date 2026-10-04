package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateErrorDetectionRule_EqualsValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "ERROR", ""},
		{"integer", 0, ""},
		{"float", 1.5, ""},
		{"boolean", true, ""},
		{"missing", nil, "equals rule requires a value"},
		{"map", map[string]any{"kind": "x"}, "equals value must be a string, number, or boolean"},
		{"list", []any{"a"}, "equals value must be a string, number, or boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateErrorDetectionRule(ErrorDetectionRule{Path: "status", Rule: "equals", Value: tt.value})
			if tt.want == "" {
				assert.Empty(t, errs)
				return
			}
			assert.Contains(t, errs, tt.want)
		})
	}
}

func TestErrorStatus_StatusFor(t *testing.T) {
	m := ErrorStatus{Status: 500, Categories: map[string]int{"VALIDATION": 400, " Temporary ": 503}}
	for category, want := range map[string]int{
		"VALIDATION": 400,
		"validation": 400,
		"temporary":  503,
		" TEMPORARY": 503,
		"UNKNOWN":    500,
		"":           500,
	} {
		assert.Equal(t, want, m.StatusFor(category), category)
	}
	assert.Equal(t, 0, ErrorStatus{Categories: map[string]int{"VALIDATION": 400}}.StatusFor("OTHER"), "no status for an unlisted category")
	assert.Equal(t, 0, ErrorStatus{}.StatusFor("VALIDATION"))
}

func TestErrorStatus_DescribeStatuses(t *testing.T) {
	assert.Equal(t, "TEMPORARY 503, VALIDATION 400, otherwise 500",
		ErrorStatus{Status: 500, Categories: map[string]int{"VALIDATION": 400, "TEMPORARY": 503}}.DescribeStatuses())
	assert.Equal(t, "500", ErrorStatus{Status: 500}.DescribeStatuses())
	assert.Equal(t, "VALIDATION 400", ErrorStatus{Categories: map[string]int{"VALIDATION": 400}}.DescribeStatuses())
	assert.Equal(t, "", ErrorStatus{}.DescribeStatuses())
}

func TestValidate_ErrorStatus(t *testing.T) {
	withCategory := &ErrorDetailMapping{Category: "result.errors.0.type"}
	tests := []struct {
		name  string
		rules []ErrorDetectionRule
		graph *ErrorStatus
		want  []string
	}{
		{name: "rule status and categories", rules: []ErrorDetectionRule{{Path: "result.errors", Rule: "non-empty", Details: withCategory,
			ErrorStatus: ErrorStatus{Status: 500, Categories: map[string]int{"VALIDATION": 400}}}}},
		{name: "graph mapping", rules: []ErrorDetectionRule{{Path: "result.errors", Rule: "non-empty", Details: withCategory}},
			graph: &ErrorStatus{Status: 500, Categories: map[string]int{"TEMPORARY": 503}}},
		{name: "a success status", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists", ErrorStatus: ErrorStatus{Status: 200}}},
			want: []string{"errorDetection[0]: status 200 is not an error status (use 400 to 599)"}},
		{name: "a category out of range", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists", Details: withCategory,
			ErrorStatus: ErrorStatus{Categories: map[string]int{"VALIDATION": 600}}}},
			want: []string{"errorDetection[0]: categories: VALIDATION: status 600 is not an error status (use 400 to 599)"}},
		{name: "categories without a category path", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists",
			ErrorStatus: ErrorStatus{Categories: map[string]int{"VALIDATION": 400}}}},
			want: []string{"errorDetection[0]: categories needs details.category, the path the error's category is read from"}},
		{name: "the same category twice", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists", Details: withCategory,
			ErrorStatus: ErrorStatus{Categories: map[string]int{"VALIDATION": 400, "validation": 422}}}},
			want: []string{`errorDetection[0]: categories: "VALIDATION" and "validation" are the same category (categories match without case)`}},
		{name: "an unnamed category", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists", Details: withCategory,
			ErrorStatus: ErrorStatus{Categories: map[string]int{" ": 400}}}},
			want: []string{"errorDetection[0]: categories: a category needs a name"}},
		{name: "an empty graph mapping", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists"}}, graph: &ErrorStatus{},
			want: []string{"errorStatus: needs a status, categories, or both"}},
		{name: "a graph status out of range", rules: []ErrorDetectionRule{{Path: "e", Rule: "exists"}}, graph: &ErrorStatus{Status: 302},
			want: []string{"errorStatus: status 302 is not an error status (use 400 to 599)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Graph{Version: "1.0.0", ErrorDetection: tt.rules, ErrorStatus: tt.graph, Nodes: map[string]*Node{
				"n": {Name: "n", Adapter: "n", Outputs: []Output{{Name: "y", Type: "string"}}},
			}}
			err := Validate(g)
			if len(tt.want) == 0 {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.want, verr.Errors)
		})
	}

	node := &Graph{Version: "1.0.0", Nodes: map[string]*Node{"n": {Name: "n", Adapter: "n",
		ErrorDetection: []ErrorDetectionRule{{Path: "e", Rule: "exists", ErrorStatus: ErrorStatus{Status: 99}}}}}}
	assert.ErrorContains(t, Validate(node), `node "n": errorDetection[0]: status 99 is not an error status`)
}

func TestValidateWarnings_ErrorStatus(t *testing.T) {
	g := &Graph{Version: "1.0.0", ErrorStatus: &ErrorStatus{Status: 500}, Nodes: map[string]*Node{"n": {Name: "n", Adapter: "n"}}}
	assert.Contains(t, ValidateWarnings(g), "errorStatus is set, but the graph has no errorDetection rules to give a status to")

	g.ErrorStatus.Categories = map[string]int{"VALIDATION": 400}
	g.Nodes["n"].ErrorDetection = []ErrorDetectionRule{{Path: "e", Rule: "exists"}}
	assert.Contains(t, ValidateWarnings(g), "errorStatus.categories: no errorDetection rule reads a category (details.category), so only errorStatus.status applies")

	g.Nodes["n"].ErrorDetection[0].Details = &ErrorDetailMapping{Category: "e.type"}
	for _, w := range ValidateWarnings(g) {
		assert.NotContains(t, w, "errorStatus")
	}
}
