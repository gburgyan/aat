package engine

import (
	"testing"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRelaxationTracker_DefaultDepth(t *testing.T) {
	tracker := NewRelaxationTracker(0)
	assert.Equal(t, 0, tracker.Depth())

	// Should allow 3 relaxations (default)
	for i := 0; i < 3; i++ {
		require.NoError(t, tracker.CanRelax("c"+string(rune('0'+i))))
		tracker.Relax("c"+string(rune('0'+i)), "node.input", "test")
	}
	assert.Equal(t, 3, tracker.Depth())

	// 4th should fail (budget)
	err := tracker.CanRelax("c3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "budget exhausted")
}

func TestNewRelaxationTracker_CustomDepth(t *testing.T) {
	tracker := NewRelaxationTracker(2)

	tracker.Relax("c0", "node.input", "test")
	tracker.Relax("c1", "node.input", "test")

	err := tracker.CanRelax("c2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "budget exhausted")
}

func TestRelaxationTracker_CircularDetection(t *testing.T) {
	tracker := NewRelaxationTracker(5)

	require.NoError(t, tracker.CanRelax("date_constraint"))
	tracker.Relax("date_constraint", "searchFlights.departureDate", "resolution_exhausted")

	// Same constraint again → circular
	err := tracker.CanRelax("date_constraint")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already relaxed")
	assert.Contains(t, err.Error(), "circular")
}

func TestRelaxationTracker_IsRelaxed(t *testing.T) {
	tracker := NewRelaxationTracker(3)

	assert.False(t, tracker.IsRelaxed("date_constraint"))

	tracker.Relax("date_constraint", "node.input", "test")

	assert.True(t, tracker.IsRelaxed("date_constraint"))
	assert.False(t, tracker.IsRelaxed("other_constraint"))
}

func TestRelaxationTracker_Records(t *testing.T) {
	tracker := NewRelaxationTracker(5)

	// Empty initially
	assert.Nil(t, tracker.Records())

	tracker.Relax("c1", "searchFlights.origin", "resolution_exhausted")
	tracker.Relax("c2", "searchFlights.destination", "filter_empty")

	records := tracker.Records()
	require.Len(t, records, 2)

	assert.Equal(t, "c1", records[0].ConstraintName)
	assert.Equal(t, "searchFlights.origin", records[0].InputRef)
	assert.Equal(t, "resolution_exhausted", records[0].Reason)
	assert.Equal(t, 1, records[0].Depth)

	assert.Equal(t, "c2", records[1].ConstraintName)
	assert.Equal(t, 2, records[1].Depth)

	// Records returns a copy
	records[0].ConstraintName = "modified"
	assert.Equal(t, "c1", tracker.Records()[0].ConstraintName)
}

func TestRelaxationTracker_Depth(t *testing.T) {
	tracker := NewRelaxationTracker(5)
	assert.Equal(t, 0, tracker.Depth())

	tracker.Relax("c1", "ref", "reason")
	assert.Equal(t, 1, tracker.Depth())

	tracker.Relax("c2", "ref", "reason")
	assert.Equal(t, 2, tracker.Depth())
}

func TestFindSoftConstraintForInput_FullMatch(t *testing.T) {
	p := &plan.Plan{
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{
						Type:      "date_range",
						Name:      "travel_date",
						AppliesTo: []string{"searchFlights.departureDate"},
					},
				},
			},
		},
	}

	sc, found := FindSoftConstraintForInput(p, "searchFlights", "departureDate")
	assert.True(t, found)
	assert.Equal(t, "travel_date", sc.Name)
}

func TestFindSoftConstraintForInput_NodeOnlyMatch(t *testing.T) {
	p := &plan.Plan{
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{
						Type:      "carrier_preference",
						Name:      "preferred_carrier",
						AppliesTo: []string{"searchFlights"},
					},
				},
			},
		},
	}

	sc, found := FindSoftConstraintForInput(p, "searchFlights", "carrier")
	assert.True(t, found)
	assert.Equal(t, "preferred_carrier", sc.Name)
}

func TestFindSoftConstraintForInput_NoMatch(t *testing.T) {
	p := &plan.Plan{
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Soft: []plan.Constraint{
					{
						Type:      "date_range",
						Name:      "travel_date",
						AppliesTo: []string{"searchFlights.departureDate"},
					},
				},
			},
		},
	}

	_, found := FindSoftConstraintForInput(p, "bookFlight", "origin")
	assert.False(t, found)
}

func TestFindSoftConstraintForInput_NilPlan(t *testing.T) {
	_, found := FindSoftConstraintForInput(nil, "node", "input")
	assert.False(t, found)
}

func TestFindSoftConstraintForInput_NilConstraints(t *testing.T) {
	p := &plan.Plan{}
	_, found := FindSoftConstraintForInput(p, "node", "input")
	assert.False(t, found)
}

func TestFindSoftConstraintForInput_HardConstraintsIgnored(t *testing.T) {
	p := &plan.Plan{
		Intent: plan.Intent{
			Constraints: &plan.Constraints{
				Hard: []plan.Constraint{
					{
						Type:      "exact_airport",
						Name:      "origin_airport",
						AppliesTo: []string{"searchFlights.origin"},
					},
				},
			},
		},
	}

	_, found := FindSoftConstraintForInput(p, "searchFlights", "origin")
	assert.False(t, found)
}

func TestRelaxationTracker_NegativeDepthDefaults(t *testing.T) {
	tracker := NewRelaxationTracker(-1)
	// Should default to 3
	for i := 0; i < 3; i++ {
		require.NoError(t, tracker.CanRelax("c"+string(rune('0'+i))))
		tracker.Relax("c"+string(rune('0'+i)), "ref", "reason")
	}
	err := tracker.CanRelax("c3")
	assert.Error(t, err)
}

func TestSplitAppliesTo(t *testing.T) {
	tests := []struct {
		ref       string
		wantNode  string
		wantInput string
		wantOK    bool
	}{
		{"searchFlights.origin", "searchFlights", "origin", true},
		{"searchFlights", "searchFlights", "", false},
		{"a.b.c", "a", "b.c", true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			node, input, ok := splitAppliesTo(tt.ref)
			assert.Equal(t, tt.wantNode, node)
			assert.Equal(t, tt.wantInput, input)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
