package fuzz

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/adapter"
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
	rank := func(m string) int { return slices.Index(plan.FuzzModes, m) }
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

func TestGenerate_AbsenceAndTemplateCases(t *testing.T) {
	target := addItemTarget()
	target.Template = &adapter.Template{Protocol: "http", Request: adapter.TemplateRequest{
		Method: "POST", Path: "/carts/{{cartId}}/items",
		Body: `{"cartId": "{{cartId}}", "sku": "{{sku}}", "quantity": {{quantity}}, "channel": "web",
		  "meta": {"source": "aat"}{{?note}}, "note": "{{note}}"{{/note}}}`,
	}}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)

	missing := byID["quantity.missing"]
	assert.Equal(t, plan.FuzzNegative, missing.Mode, "a required input left out")
	assert.Equal(t, []plan.RequestPatch{{Where: "body", Path: "quantity", Op: "remove"}}, missing.Patch)
	assert.Equal(t, plan.FuzzNegative, byID["quantity.null"].Mode)
	assert.Equal(t, []plan.RequestPatch{{Where: "body", Path: "quantity", Op: "set"}}, byID["quantity.null"].Patch)
	assert.NotContains(t, byID, "cartId.missing", "a wired input is left alone")
	assert.NotContains(t, byID, "note.missing", "an input sent only in a conditional block is left out by the template itself")

	for id, strategy := range map[string]string{
		"body.channel.remove":     "remove",
		"body.channel.null":       "null",
		"body.channel.wrong-type": "wrong-type",
		"body.meta.empty":         "empty",
		"body.meta.remove":        "remove",
		"body.meta.source.remove": "remove",
		"body.extra-property":     "extra-property",
	} {
		c, ok := byID[id]
		if assert.True(t, ok, "missing case %s", id) {
			assert.Equal(t, plan.FuzzEdge, c.Mode, id)
			assert.Equal(t, strategy, c.Strategy, id)
			assert.Empty(t, c.Input, id)
		}
	}
	assert.Equal(t, 12345, byID["body.channel.wrong-type"].Patch[0].Value)

	// Naming inputs limits the cases to them: no template fields.
	only, err := Generate(target, Options{Inputs: []string{"quantity"}})
	require.NoError(t, err)
	for _, c := range only {
		assert.Equal(t, "quantity", c.Input, c.ID)
	}
}

func TestGenerate_OptionalInputMissingIsPositive(t *testing.T) {
	node := &graph.Node{Name: "search", Inputs: []graph.Input{
		{Name: "limit", Type: "integer", Optional: true, Default: &graph.InputDefault{Value: 10}},
		{Name: "cursor", Type: "string", Optional: true},
	}}
	target := Target{Node: node, Step: plan.Step{ID: "s", Node: "search", Values: map[string]plan.StepValue{"limit": {Default: 10}}},
		Template: &adapter.Template{Request: adapter.TemplateRequest{Method: "GET", Path: "/search?limit={{limit}}&cursor={{cursor}}"}}}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)
	assert.Equal(t, plan.FuzzPositive, byID["limit.missing"].Mode)
	assert.Equal(t, []plan.RequestPatch{{Where: "query", Path: "limit", Op: "remove"}}, byID["limit.missing"].Patch)
	assert.NotContains(t, byID, "limit.null", "a query parameter can't be null")
	assert.NotContains(t, byID, "cursor.missing", "an optional input with no value is left out already")
}

// TestGenerate_CapAfterSkipAndOnly checks that the cap draws from the cases
// left after Skip and Only, so it never spends picks on skipped inputs or
// drops a case Only names.
func TestGenerate_CapAfterSkipAndOnly(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	for seed := uint64(1); seed <= 20; seed++ {
		all, err := Generate(addItemTarget(), Options{Skip: []string{"note"}, Now: now})
		require.NoError(t, err)
		cases, err := Generate(addItemTarget(), Options{Skip: []string{"note"}, Max: 10, Seed: seed, Now: now})
		require.NoError(t, err)
		assert.Greater(t, len(all), 10)
		assert.Len(t, cases, 10)
		for _, c := range cases {
			assert.NotEqual(t, "note", c.Input, c.ID)
		}

		cases, err = Generate(addItemTarget(), Options{Only: []string{"quantity.below-min", "quantity.above-max"}, Max: 5, Seed: seed, Now: now})
		require.NoError(t, err)
		assert.Len(t, cases, 2, "both named cases, whatever the seed")
	}
}

// TestGenerate_GRPCLeavesOutUnencodableValues checks that a gRPC target gets
// no case its message can't carry: every one would be not-sent.
func TestGenerate_GRPCLeavesOutUnencodableValues(t *testing.T) {
	target := addItemTarget()
	target.Template = &adapter.Template{Protocol: adapter.ProtocolGRPC, Request: adapter.TemplateRequest{
		Message: `{"cartId": "{{cartId}}", "quantity": {{quantity}}, "channel": "web"}`,
	}}
	cases, err := Generate(target, Options{Now: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	byID := caseByID(cases)
	for _, id := range []string{"quantity.wrong-type", "quantity.fraction", "quantity.overflow", "gift.wrong-type", "body.channel.wrong-type"} {
		assert.NotContains(t, byID, id)
	}
	assert.Contains(t, byID, "quantity.above-max")
	assert.Contains(t, byID, "body.channel.remove")
}

// TestGenerate_PositiveCasesKeepToThePattern checks that a positive length
// case is one the input's pattern allows, and that a pool value the pattern
// refuses is not taken as allowed.
func TestGenerate_PositiveCasesKeepToThePattern(t *testing.T) {
	target := Target{
		Node: &graph.Node{Name: "search", Inputs: []graph.Input{
			{Name: "origin", Type: "string", Constraints: &graph.Constraint{MinLength: iptr(3), MaxLength: iptr(3), Pattern: "^[A-Z]{3}$"}},
			{Name: "sku", Type: "string", Constraints: &graph.Constraint{MinLength: iptr(9), Pattern: "^SKU-[0-9]{5}$"}},
		}},
		Step: plan.Step{ID: "search", Node: "search", Values: map[string]plan.StepValue{
			"origin": {Pool: []any{"JFK", "lax"}},
		}},
	}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)
	assert.Equal(t, "AAA", byID["origin.at-min-length"].Value)
	assert.Equal(t, plan.FuzzPositive, byID["origin.at-min-length"].Mode)
	assert.Equal(t, plan.FuzzPositive, byID["origin.pool-1"].Mode)
	assert.Equal(t, plan.FuzzEdge, byID["origin.pool-2"].Mode, "lax breaks the pattern")
	assert.NotContains(t, byID, "sku.at-min-length", "no run of one character fits the pattern")
	assert.Equal(t, plan.FuzzNegative, byID["sku.below-min-length"].Mode)
}

// TestGenerate_HugeLengthsGetNoLengthCases checks that a maxLength or
// minLength too long to send gets no length case, rather than a string of
// gigabytes built before anything filters the cases.
func TestGenerate_HugeLengthsGetNoLengthCases(t *testing.T) {
	for _, c := range []*graph.Constraint{
		{MinLength: iptr(1), MaxLength: iptr(2147483647)},
		{MinLength: iptr(math.MaxInt), MaxLength: iptr(math.MaxInt)},
	} {
		target := Target{
			Node: &graph.Node{Name: "n", Inputs: []graph.Input{{Name: "name", Type: "string", Constraints: c}}},
			Step: plan.Step{ID: "n", Node: "n"},
		}
		cases, err := Generate(target, Options{Only: []string{"name.empty"}})
		require.NoError(t, err)
		assert.Len(t, cases, 1)
		cases, err = Generate(target, Options{})
		require.NoError(t, err)
		for _, fc := range cases {
			if s, ok := fc.Value.(string); ok {
				assert.LessOrEqual(t, len(s), maxCaseLength, fc.ID)
			}
		}
	}
}

// TestGenerate_IntegerBoundsPastAFloat checks that an integer bound no int64
// holds, or one too large for a float to step past, gets no boundary case
// that would send the wrong number.
func TestGenerate_IntegerBoundsPastAFloat(t *testing.T) {
	target := Target{
		Node: &graph.Node{Name: "n", Inputs: []graph.Input{
			{Name: "big", Type: "integer", Constraints: &graph.Constraint{Min: f64(math.MinInt64), Max: f64(math.MaxInt64)}},
		}},
		Step: plan.Step{ID: "n", Node: "n"},
	}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)
	assert.Equal(t, int64(math.MinInt64), byID["big.at-min"].Value)
	assert.NotContains(t, byID, "big.below-min", "a float can't hold one less")
	assert.NotContains(t, byID, "big.at-max", "9223372036854775807 reads as 2^63, which no int64 holds")
	assert.NotContains(t, byID, "big.above-max")
}

// TestGenerate_ValuesTheRequestCantCarry checks that a value case is left out
// when the template would not send it as itself: "" where an empty input is
// left out, and a control character in a header.
func TestGenerate_ValuesTheRequestCantCarry(t *testing.T) {
	target := Target{
		Node: &graph.Node{Name: "list", Inputs: []graph.Input{
			{Name: "category", Type: "enum[toys, books]", Optional: true},
			{Name: "hint", Type: "string"},
			{Name: "name", Type: "string"},
		}},
		Step: plan.Step{ID: "list", Node: "list"},
		Template: &adapter.Template{Request: adapter.TemplateRequest{
			Method:  "POST",
			Path:    "/products{{?category}}?category={{category}}{{/category}}",
			Headers: map[string]string{"X-Hint": "{{hint}}"},
			Body:    `{"name": "{{name}}"}`,
		}},
	}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)
	assert.NotContains(t, byID, "category.empty", "an empty category leaves the parameter out")
	assert.Contains(t, byID, "category.not-in-enum")
	assert.NotContains(t, byID, "hint.empty", "a header that is one placeholder is left out when empty")
	assert.NotContains(t, byID, "hint.control-chars", "HTTP refuses it before sending")
	assert.Contains(t, byID, "hint.unicode", "HTTP allows bytes above ASCII")
	assert.Contains(t, byID, "name.empty", "the body sends an empty name as itself")
	assert.Contains(t, byID, "name.control-chars")

	target.Template.Protocol = adapter.ProtocolGRPC
	target.Template.Request = adapter.TemplateRequest{Metadata: map[string]string{"x-hint": "{{hint}}"}, Message: `{"name": "{{name}}"}`}
	cases, err = Generate(target, Options{})
	require.NoError(t, err)
	byID = caseByID(cases)
	assert.NotContains(t, byID, "hint.unicode", "gRPC metadata is printable ASCII")
	assert.Contains(t, byID, "hint.sql-quote")
}

// TestGenerate_InputNamedBody checks that an input named body keeps its
// name, so skip and --fuzz-input find it.
func TestGenerate_InputNamedBody(t *testing.T) {
	target := Target{
		Node: &graph.Node{Name: "post", Inputs: []graph.Input{{Name: "body", Type: "string"}}},
		Step: plan.Step{ID: "post", Node: "post"},
		Template: &adapter.Template{Request: adapter.TemplateRequest{
			Method: "POST", Path: "/notes", Body: `{"body": "{{body}}", "kind": "note"}`,
		}},
	}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	byID := caseByID(cases)
	assert.Equal(t, "body", byID["body.missing"].Input)
	assert.Equal(t, "", byID["body.kind.remove"].Input, "a template field's case names no input")

	cases, err = Generate(target, Options{Skip: []string{"body"}})
	require.NoError(t, err)
	for _, c := range cases {
		assert.NotEqual(t, "body", c.Input, c.ID)
	}
}

// TestGenerate_ExpressionWiredInputIsLeftAlone checks that an input whose
// default reads an earlier step's output is wired, as a from is.
func TestGenerate_ExpressionWiredInputIsLeftAlone(t *testing.T) {
	target := addItemTarget()
	target.Step.Values["cartId"] = plan.StepValue{Default: "{{createCart.cartId}}"}
	target.Step.Values["sku"] = plan.StepValue{Default: "{{today}}"}
	cases, err := Generate(target, Options{})
	require.NoError(t, err)
	for _, c := range cases {
		assert.NotEqual(t, "cartId", c.Input, c.ID)
	}
	assert.Contains(t, caseByID(cases), "sku.empty", "an expression that reads no step is not wiring")
}

// TestGenerate_EscapedDotIsNotAnIndex checks that a template field under a
// key with an escaped dot, such as v1\.2, is fuzzed: it is not an array
// element.
func TestGenerate_EscapedDotIsNotAnIndex(t *testing.T) {
	assert.True(t, inArray("items.0.sku"))
	assert.False(t, inArray(`versions.v1\.2`))
	assert.False(t, inArray(`v1\.2`))
}
