package archive

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
