package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/internal/yamlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckErrorDetection_EqualsWithYAMLValues decodes rules as a graph file
// would, so each value has the Go type YAML gives it (int, float64, string,
// bool, map), and checks them against JSON bodies.
func TestCheckErrorDetection_EqualsWithYAMLValues(t *testing.T) {
	var file struct {
		Rules []graph.ErrorDetectionRule `yaml:"rules"`
	}
	require.NoError(t, yamlx.Decode([]byte(`
rules:
  - {path: code, rule: equals, value: 0}
  - {path: ratio, rule: equals, value: 0.5}
  - {path: status, rule: equals, value: ERROR}
  - {path: failed, rule: equals, value: true}
  - {path: detail, rule: equals, value: {kind: x}}
`), &file))
	require.Len(t, file.Rules, 5)

	tests := []struct {
		name string
		rule int
		body string
		want bool
	}{
		{"an integer matches a JSON number", 0, `{"code": 0}`, true},
		{"an integer matches a JSON number written as a float", 0, `{"code": 0.0}`, true},
		{"an integer does not match a string", 0, `{"code": "0"}`, false},
		{"a float matches", 1, `{"ratio": 0.5}`, true},
		{"a string matches", 2, `{"status": "ERROR"}`, true},
		{"a boolean matches", 3, `{"failed": true}`, true},
		{"an object value does not panic", 4, `{"detail": {"kind": "y"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckErrorDetection(file.Rules[tt.rule:tt.rule+1], []byte(tt.body))
			assert.Equal(t, tt.want, got != nil)
		})
	}
}
