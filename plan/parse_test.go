package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFile_Minimal(t *testing.T) {
	p, err := ParseFile("testdata/valid/minimal.yaml")
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 1)

	step := p.Execution.Steps[0]
	assert.Equal(t, "searchFlights", step.Node)
	assert.Len(t, step.Values, 3)

	// Bare scalar values should set Default
	assert.Equal(t, "DEN", step.Values["origin"].Default)
	assert.Equal(t, "SFO", step.Values["destination"].Default)
	assert.Equal(t, "2026-03-15", step.Values["departureDate"].Default)
}

func TestParseFile_TravelportBooking(t *testing.T) {
	p, err := ParseFile("testdata/valid/travelport_booking.yaml")
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 6)

	// Metadata
	assert.Equal(t, "Book a flight from DEN to SFO", p.Metadata.Prompt)
	assert.Equal(t, "1.0.0", p.Metadata.GraphVersion)
	assert.False(t, p.Metadata.Created.IsZero())

	// Intent
	assert.Equal(t, "commitBooking", p.Intent.Goal)

	// Step with named selection
	priceStep := p.Execution.Steps[1]
	assert.Equal(t, "priceOffer", priceStep.Node)
	assert.Equal(t, []string{"searchFlights"}, priceStep.DependsOn)

	require.Contains(t, priceStep.Selections, "catalogOffering")
	assert.Equal(t, "searchFlights.catalogOfferings", priceStep.Selections["catalogOffering"].From)
	assert.Equal(t, "first", priceStep.Selections["catalogOffering"].Strategy)

	offeringVal := priceStep.Values["offeringId"]
	assert.Equal(t, "catalogOffering.offeringId", offeringVal.FromSelection)
	productVal := priceStep.Values["productRef"]
	assert.Equal(t, "catalogOffering.productRef", productVal.FromSelection)

	// Step with isGoal
	commitStep := p.Execution.Steps[5]
	assert.Equal(t, "commitBooking", commitStep.Node)
	assert.True(t, commitStep.IsGoal)
	assert.Equal(t, []string{"addOffer", "addTraveler", "createWorkbench"}, commitStep.DependsOn)
}

func TestParseFile_FullFeatured(t *testing.T) {
	p, err := ParseFile("testdata/valid/full_featured.yaml")
	require.NoError(t, err)

	// Intent with constraints
	require.NotNil(t, p.Intent.Constraints)
	assert.Len(t, p.Intent.Constraints.Hard, 1)
	assert.Len(t, p.Intent.Constraints.Soft, 1)
	assert.Len(t, p.Intent.Constraints.Free, 1)

	// Step with retry
	search := p.Execution.Steps[0]
	require.NotNil(t, search.Retry)
	assert.Equal(t, 3, search.Retry.Max)
	assert.Equal(t, []string{"timeout", "503"}, search.Retry.On)

	// Step with assertions
	require.NotNil(t, search.Assertions)
	assert.Len(t, search.Assertions.Mechanical, 2)
	assert.Len(t, search.Assertions.Semantic, 1)
	assert.Equal(t, "status", search.Assertions.Mechanical[0].Type)
	assert.Equal(t, 200, search.Assertions.Mechanical[0].Expect)

	// Step with fallback
	price := p.Execution.Steps[1]
	require.NotNil(t, price.Fallback)
	assert.Equal(t, "nextValue", price.Fallback.Action)
	assert.Equal(t, 3, price.Fallback.MaxAttempts)

	// Cleanup steps
	require.Len(t, p.Execution.Cleanup, 1)
	assert.Equal(t, "ignoreWorkbench", p.Execution.Cleanup[0].Node)
	assert.Equal(t, "always", p.Execution.Cleanup[0].RunOn)
}

func TestParse_NoSteps(t *testing.T) {
	_, err := ParseFile("testdata/invalid/no_steps.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one execution step")
}

func TestParse_BadYAML(t *testing.T) {
	_, err := ParseFile("testdata/invalid/bad_yaml.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YAML parse error")
}

func TestParse_MissingFile(t *testing.T) {
	_, err := ParseFile("testdata/nonexistent.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading plan file")
}

func TestStepValue_BareString(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        name: "hello"
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["name"]
	assert.Equal(t, "hello", sv.Default)
	assert.Nil(t, sv.Select)
	assert.Empty(t, sv.From)
}

func TestStepValue_BareInt(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        count: 42
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["count"]
	assert.Equal(t, 42, sv.Default)
}

func TestStepValue_BareBool(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        flag: true
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["flag"]
	assert.Equal(t, true, sv.Default)
}

func TestStepValue_FullStruct(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        item:
          from: upstream.items
          select:
            strategy: first
            field: "id"
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["item"]
	assert.Equal(t, "upstream.items", sv.From)
	require.NotNil(t, sv.Select)
	assert.Equal(t, "first", sv.Select.Strategy)
	assert.Equal(t, "id", sv.Select.Field)
	assert.Nil(t, sv.Default)
}

func TestStepValue_DefaultWithFallbackPool(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        origin:
          default: "DEN"
          fallbackPool: ["LAX", "ORD", "ATL"]
          fallbackStrategy: "sequential"
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["origin"]
	assert.Equal(t, "DEN", sv.Default)
	assert.Equal(t, []any{"LAX", "ORD", "ATL"}, sv.FallbackPool)
	require.NotNil(t, sv.FallbackStrategy)
	assert.Equal(t, "sequential", *sv.FallbackStrategy)
}

func TestAssertions_FlatList(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      assertions:
        - type: status
          expect: 200
        - type: fieldExists
          path: "$.locator"
`)
	p, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, p.Execution.Steps[0].Assertions)
	assert.Len(t, p.Execution.Steps[0].Assertions.Mechanical, 2)
	assert.Equal(t, "status", p.Execution.Steps[0].Assertions.Mechanical[0].Type)
	assert.Equal(t, 200, p.Execution.Steps[0].Assertions.Mechanical[0].Expect)
	assert.Equal(t, "fieldExists", p.Execution.Steps[0].Assertions.Mechanical[1].Type)
}

func TestAssertions_MappingForm(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      assertions:
        mechanical:
          - type: status
            expect: 200
        semantic:
          - "response should contain valid data"
`)
	p, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, p.Execution.Steps[0].Assertions)
	assert.Len(t, p.Execution.Steps[0].Assertions.Mechanical, 1)
	assert.Len(t, p.Execution.Steps[0].Assertions.Semantic, 1)
}

func TestStepValue_SelectWithIndex(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        item:
          from: upstream.items
          select:
            strategy: index
            index: 3
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["item"]
	require.NotNil(t, sv.Select)
	assert.Equal(t, "index", sv.Select.Strategy)
	assert.Equal(t, 3, sv.Select.Index)
}

func TestParseFile_NamedSelections(t *testing.T) {
	p, err := ParseFile("testdata/valid/named_selections.yaml")
	require.NoError(t, err)
	require.Len(t, p.Execution.Steps, 6)

	// priceOffer step should have selections
	priceStep := p.Execution.Steps[1]
	assert.Equal(t, "priceOffer", priceStep.Node)
	require.Len(t, priceStep.Selections, 1)

	offering := priceStep.Selections["offering"]
	assert.Equal(t, "searchFlights.catalogOfferings", offering.From)
	assert.Equal(t, "first", offering.Strategy)

	// Values use fromSelection
	offeringId := priceStep.Values["offeringId"]
	assert.Equal(t, "offering.offeringId", offeringId.FromSelection)
	assert.Empty(t, offeringId.From)
	assert.Nil(t, offeringId.Select)

	productRef := priceStep.Values["productRef"]
	assert.Equal(t, "offering.productRef", productRef.FromSelection)
}

func TestParse_NamedSelectionsInline(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      selections:
        result:
          from: upstream.items
          strategy: match
          filter: "type == 'good'"
      values:
        id:
          fromSelection: result.id
        name:
          fromSelection: result.name
`)
	p, err := Parse(data)
	require.NoError(t, err)

	step := p.Execution.Steps[0]
	require.Len(t, step.Selections, 1)
	sel := step.Selections["result"]
	assert.Equal(t, "upstream.items", sel.From)
	assert.Equal(t, "match", sel.Strategy)
	assert.Equal(t, "type == 'good'", sel.Filter)

	assert.Equal(t, "result.id", step.Values["id"].FromSelection)
	assert.Equal(t, "result.name", step.Values["name"].FromSelection)
}

func TestStepValue_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		sv       StepValue
		expected bool
	}{
		{"zero value", StepValue{}, true},
		{"with default", StepValue{Default: "DEN"}, false},
		{"with from", StepValue{From: "upstream.output"}, false},
		{"with fromSelection", StepValue{FromSelection: "sel.field"}, false},
		{"with select", StepValue{Select: &SelectionConfig{Strategy: "first"}}, false},
		{"with constraint", StepValue{Constraint: "value > 0"}, false},
		{"with fallback pool", StepValue{FallbackPool: []any{"a", "b"}}, false},
		{"with fallback strategy", StepValue{FallbackStrategy: strPtr("random")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.sv.IsEmpty())
		})
	}
}

func TestStepValue_EmptyMapping_Unmarshal(t *testing.T) {
	// YAML `returnDate: {}` should produce an empty StepValue
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        origin: DEN
        returnDate: {}
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["returnDate"]
	assert.True(t, sv.IsEmpty(), "empty YAML mapping should produce an empty StepValue")
	assert.False(t, p.Execution.Steps[0].Values["origin"].IsEmpty(), "literal value should not be empty")
}

func strPtr(s string) *string { return &s }

// --- Plan-level auth and headers tests ---

func TestParse_WithAuth(t *testing.T) {
	data := []byte(`
auth:
  type: bearer
  credentials:
    token:
      source: literal
      value: my-token
execution:
  steps:
    - node: test
      values:
        key: val
`)
	p, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "bearer", p.Auth.Type)
	assert.Equal(t, "literal", p.Auth.Credentials["token"].Source)
	assert.Equal(t, "my-token", p.Auth.Credentials["token"].Value)
}

func TestParse_WithHeaders(t *testing.T) {
	data := []byte(`
headers:
  Content-Version: "2.0"
  X-Test-Customer: customer-x
execution:
  steps:
    - node: test
      values:
        key: val
`)
	p, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, p.Headers, 2)
	assert.Equal(t, "2.0", p.Headers["Content-Version"])
	assert.Equal(t, "customer-x", p.Headers["X-Test-Customer"])
}

func TestParse_WithAuthAndHeaders(t *testing.T) {
	data := []byte(`
auth:
  type: oauth2
  tokenUrl: https://auth.example.com/token
  credentials:
    username:
      source: literal
      value: user1
    password:
      source: literal
      value: pass1
    clientId:
      source: literal
      value: cid
    clientSecret:
      source: literal
      value: csec
headers:
  X-Custom: custom-value
execution:
  steps:
    - node: test
      values:
        key: val
`)
	p, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "oauth2", p.Auth.Type)
	assert.Equal(t, "https://auth.example.com/token", p.Auth.TokenURL)
	assert.Equal(t, "custom-value", p.Headers["X-Custom"])
}

func TestParse_WithoutAuthOrHeaders(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      values:
        key: val
`)
	p, err := Parse(data)
	require.NoError(t, err)
	assert.Nil(t, p.Auth)
	assert.Empty(t, p.Headers)
}

func TestStepValue_FromSelectionNoWholeElement(t *testing.T) {
	data := []byte(`
execution:
  steps:
    - node: test
      selections:
        result:
          from: upstream.items
      values:
        item:
          fromSelection: result
`)
	p, err := Parse(data)
	require.NoError(t, err)
	sv := p.Execution.Steps[0].Values["item"]
	assert.Equal(t, "result", sv.FromSelection)
}
