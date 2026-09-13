package engine

import (
	"testing"

	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

func TestNoteDefaultOrigin(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		value      plan.StepValue
		wantSource string
		wantLayer  string
	}{
		{"a plan value stays plan_default", "plan_default", plan.StepValue{Default: "x"}, "plan_default", ""},
		{"a graph default", "plan_default", plan.StepValue{Default: "x", Origin: "graph"}, "graph_default", ""},
		{"a layer's value", "plan_default", plan.StepValue{Default: "jpy", Origin: "layer", Layer: "currency-jpy"}, "layer", "currency-jpy"},
		{"a layer's expression keeps its source and names the layer", "expression", plan.StepValue{Default: "{{uuid}}", Origin: "layer", Layer: "keys"}, "expression", "keys"},
		{"a graph default's reference keeps plan_from", "plan_from", plan.StepValue{From: "a.b", Origin: "graph"}, "plan_from", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &ValueResolution{InputName: "in", Source: tt.source}
			noteDefaultOrigin(res, tt.value)
			assert.Equal(t, tt.wantSource, res.Source)
			assert.Equal(t, tt.wantLayer, res.Layer)
		})
	}
}

func TestDefaultResolution(t *testing.T) {
	assert.Equal(t, &ValueResolution{InputName: "tier", Source: "graph_default", PoolIndex: -1},
		defaultResolution("tier", &graph.InputDefault{Value: "standard"}))
	assert.Equal(t, &ValueResolution{InputName: "tier", Source: "layer", Layer: "shipping-express", PoolIndex: -1},
		defaultResolution("tier", &graph.InputDefault{Value: "express", Layer: "shipping-express"}))
}
