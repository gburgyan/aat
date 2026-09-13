package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepResolutions_JoinsSelections(t *testing.T) {
	step := &StepRecord{
		Selections: []SelectionRecord{
			{InputName: "cheapest", SelectionName: "cheapest", Strategy: "min", SortField: "price", SelectedIndex: 2},
			{InputName: "sku", SelectionName: "cheapest", Strategy: "min", SortField: "price", SelectedIndex: 2},
			{InputName: "rateId", Strategy: "first"},
		},
		Resolutions: []ValueResolutionRecord{
			{InputName: "sku", Source: "named_selection", FinalValue: "SKU-3"},
			{InputName: "rateId", Source: "select_edge", FinalValue: "rate_1", FromStep: "listShippingRates", FromOutput: "rates"},
			{InputName: "currency", Source: "plan_from", FinalValue: "USD", FromStep: "listProducts", FromOutput: "currency"},
			{InputName: "quantity", Source: "error", Error: "required input has no value"},
		},
	}

	joined := StepResolutions(step)
	require.Len(t, joined, 4)
	require.NotNil(t, joined[0].Selection)
	assert.Equal(t, "sku", joined[0].Selection.InputName, "the input's own record of the named selection")
	require.NotNil(t, joined[1].Selection)
	assert.Equal(t, "first", joined[1].Selection.Strategy)
	assert.Nil(t, joined[2].Selection)
	assert.Equal(t, "required input has no value", joined[3].Error)

	assert.Nil(t, StepResolutions(&StepRecord{}))
}

func TestSelectionTieWarnings(t *testing.T) {
	price := 19.99
	selections := []SelectionRecord{
		{InputName: "cheapest", SelectionName: "cheapest", SourceNode: "listProducts", SourceField: "products", FilteredSize: 8, Strategy: "min", SortField: "price", SortValue: &price, Ties: 3},
		{InputName: "sku", SelectionName: "cheapest", SourceNode: "listProducts", SourceField: "products", FilteredSize: 8, Strategy: "min", SortField: "price", SortValue: &price, Ties: 3},
		{InputName: "rateId", SourceNode: "listShippingRates", SourceField: "rates", FilteredSize: 4, Strategy: "max", SortField: "days", Ties: 2, SelectedIndex: 1},
		{InputName: "variantId", SourceNode: "listVariants", SourceField: "variants", FilteredSize: 3, Strategy: "min", SortField: "price", Ties: 3, OnTie: "first"},
		{InputName: "couponId", SourceNode: "listCoupons", SourceField: "coupons", FilteredSize: 3, Strategy: "first"},
	}

	assert.Equal(t, []string{
		`selection "cheapest": 3 of 8 elements from listProducts.products tie for min price at 19.99; picked index 0 (add a filter to choose, or onTie: first to accept)`,
		`input "rateId": 2 of 4 elements from listShippingRates.rates tie for max days; picked index 1 (add a filter to choose, or onTie: first to accept)`,
	}, SelectionTieWarnings(selections))
	assert.Empty(t, SelectionTieWarnings(nil))
}
