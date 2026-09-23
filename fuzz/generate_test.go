package fuzz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

func f64(v float64) *float64 { return &v }
func iptr(v int) *int        { return &v }

func addItemNode() *graph.Node {
	return &graph.Node{Name: "addItem", Inputs: []graph.Input{
		{Name: "cartId", Type: "string"},
		{Name: "sku", Type: "sku"},
		{Name: "quantity", Type: "integer", Constraints: &graph.Constraint{Min: f64(1), Max: f64(99)}},
		{Name: "gift", Type: "boolean", Optional: true},
		{Name: "tier", Type: "enum[standard, express]"},
		{Name: "note", Type: "string", Optional: true, Constraints: &graph.Constraint{MaxLength: iptr(5)}},
	}}
}

func addItemTarget() Target {
	return Target{
		Node: addItemNode(),
		Step: plan.Step{ID: "add", Node: "addItem", Values: map[string]plan.StepValue{
			"cartId":   {From: "createCart.cartId"},
			"quantity": {Default: 2},
		}},
		KB: &domain.KnowledgeBase{
			Types:      map[string]*domain.TypeDef{"sku": {Validation: "^SKU-[0-9]{4}$", Pool: "skus"}},
			ValuePools: map[string]*domain.ValuePool{"skus": {Values: []string{"SKU-1001", "SKU-1002", "SKU-1003", "SKU-1004"}}},
		},
	}
}

func caseByID(cases []plan.FuzzCase) map[string]plan.FuzzCase {
	m := map[string]plan.FuzzCase{}
	for _, c := range cases {
		m[c.ID] = c
	}
	return m
}

func TestGenerate_FromTypesAndConstraints(t *testing.T) {
	cases, err := Generate(addItemTarget(), Options{Now: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	byID := caseByID(cases)

	for id, want := range map[string]struct {
		mode  string
		value any
	}{
		"quantity.at-min":       {plan.FuzzPositive, int64(1)},
		"quantity.at-max":       {plan.FuzzPositive, int64(99)},
		"quantity.below-min":    {plan.FuzzNegative, int64(0)},
		"quantity.above-max":    {plan.FuzzNegative, int64(100)},
		"quantity.wrong-type":   {plan.FuzzNegative, `"not-a-number"`},
		"quantity.fraction":     {plan.FuzzNegative, 1.5},
		"sku.pool-1":            {plan.FuzzPositive, "SKU-1001"},
		"sku.pattern-mismatch":  {plan.FuzzNegative, "aat fuzz!"},
		"sku.sql-quote":         {plan.FuzzEdge, "' OR '1'='1"},
		"gift.true":             {plan.FuzzPositive, true},
		"tier.enum-express":     {plan.FuzzPositive, "express"},
		"tier.not-in-enum":      {plan.FuzzNegative, "aat-fuzz-not-a-member"},
		"tier.enum-wrong-case":  {plan.FuzzNegative, "STANDARD"},
		"note.at-max-length":    {plan.FuzzPositive, "aaaaa"},
		"note.above-max-length": {plan.FuzzNegative, "aaaaaa"},
	} {
		c, ok := byID[id]
		if assert.True(t, ok, "missing case %s", id) {
			assert.Equal(t, want.mode, c.Mode, id)
			assert.Equal(t, want.value, c.Value, id)
		}
	}
	assert.NotContains(t, byID, "quantity.zero", "0 is outside min 1")
	for _, c := range cases {
		assert.NotEqual(t, "cartId", c.Input, "a wired input is left alone")
	}

	// Positive cases come first, then negative, then edge.
	rank := func(m string) int { return modeRank(m) }
	for i := 1; i < len(cases); i++ {
		assert.LessOrEqual(t, rank(cases[i-1].Mode), rank(cases[i].Mode))
	}
}

func TestGenerate_Options(t *testing.T) {
	all, err := Generate(addItemTarget(), Options{})
	require.NoError(t, err)

	t.Run("modes", func(t *testing.T) {
		cases, err := Generate(addItemTarget(), Options{Modes: []string{plan.FuzzNegative}})
		require.NoError(t, err)
		require.NotEmpty(t, cases)
		for _, c := range cases {
			assert.Equal(t, plan.FuzzNegative, c.Mode)
		}
		_, err = Generate(addItemTarget(), Options{Modes: []string{"hostile"}})
		assert.ErrorContains(t, err, `unknown fuzz mode "hostile"`)
	})

	t.Run("inputs, wired ones included when named", func(t *testing.T) {
		cases, err := Generate(addItemTarget(), Options{Inputs: []string{"cartId"}})
		require.NoError(t, err)
		require.NotEmpty(t, cases)
		for _, c := range cases {
			assert.Equal(t, "cartId", c.Input)
		}
		_, err = Generate(addItemTarget(), Options{Inputs: []string{"qty"}})
		assert.ErrorContains(t, err, `no input "qty"`)
	})

	t.Run("max picks by seed, deterministically", func(t *testing.T) {
		a, err := Generate(addItemTarget(), Options{Max: 5, Seed: 1})
		require.NoError(t, err)
		b, _ := Generate(addItemTarget(), Options{Max: 5, Seed: 1})
		c, _ := Generate(addItemTarget(), Options{Max: 5, Seed: 2})
		assert.Len(t, a, 5)
		assert.Equal(t, a, b)
		assert.NotEqual(t, a, c)
		assert.Greater(t, len(all), 5)
	})
}

func TestGenerate_IDsAreUnique(t *testing.T) {
	cases, err := Generate(addItemTarget(), Options{})
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, c := range cases {
		assert.False(t, seen[c.ID], "duplicate case %s", c.ID)
		seen[c.ID] = true
	}
}
