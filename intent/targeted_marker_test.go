package intent

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
)

func TestEnrichFromTemplate_MarkerIsNotDefault(t *testing.T) {
	marker := &InputContext{InputName: "cartId"}
	enrichFromTemplate(marker, plan.Step{Values: map[string]plan.StepValue{"cartId": {Default: "AUTOWIRE"}}})
	assert.Empty(t, marker.CurrentDefault, "the prompt must not offer AUTOWIRE as the current value")

	literal := &InputContext{InputName: "cartId"}
	enrichFromTemplate(literal, plan.Step{Values: map[string]plan.StepValue{"cartId": {Default: "cart-1"}}})
	assert.Equal(t, "cart-1", literal.CurrentDefault)
}
