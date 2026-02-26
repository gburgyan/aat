package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFingerprint_NilPlan(t *testing.T) {
	assert.Equal(t, "", Fingerprint(nil))
}

func TestFingerprint_ValidHexFormat(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{{Node: "testNode", Values: map[string]StepValue{"x": {Default: "hello"}}}},
		},
	}
	fp := Fingerprint(p)
	require.NotEmpty(t, fp)
	assert.Len(t, fp, 64, "SHA-256 hex digest should be 64 characters")
	assert.Regexp(t, `^[0-9a-f]{64}$`, fp)
}

func TestFingerprint_IdenticalPlans(t *testing.T) {
	p1 := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "search", Values: map[string]StepValue{"origin": {Default: "JFK"}, "dest": {Default: "LAX"}}},
				{Node: "book", DependsOn: []string{"search"}},
			},
		},
	}
	p2 := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "search", Values: map[string]StepValue{"origin": {Default: "JFK"}, "dest": {Default: "LAX"}}},
				{Node: "book", DependsOn: []string{"search"}},
			},
		},
	}

	fp1 := Fingerprint(p1)
	fp2 := Fingerprint(p2)
	assert.Equal(t, fp1, fp2, "identical execution sections should produce the same fingerprint")
}

func TestFingerprint_DifferentValues(t *testing.T) {
	p1 := &Plan{
		Execution: Execution{
			Steps: []Step{{Node: "search", Values: map[string]StepValue{"origin": {Default: "JFK"}}}},
		},
	}
	p2 := &Plan{
		Execution: Execution{
			Steps: []Step{{Node: "search", Values: map[string]StepValue{"origin": {Default: "LAX"}}}},
		},
	}

	fp1 := Fingerprint(p1)
	fp2 := Fingerprint(p2)
	assert.NotEqual(t, fp1, fp2, "different values should produce different fingerprints")
}

func TestFingerprint_MetadataOnlyDifference(t *testing.T) {
	p1 := &Plan{
		Metadata: Metadata{Prompt: "prompt A"},
		Execution: Execution{
			Steps: []Step{{Node: "testNode", Values: map[string]StepValue{"x": {Default: "v"}}}},
		},
	}
	p2 := &Plan{
		Metadata: Metadata{Prompt: "prompt B"},
		Execution: Execution{
			Steps: []Step{{Node: "testNode", Values: map[string]StepValue{"x": {Default: "v"}}}},
		},
	}

	fp1 := Fingerprint(p1)
	fp2 := Fingerprint(p2)
	assert.Equal(t, fp1, fp2, "metadata differences should not affect fingerprint")
}

func TestFingerprint_CleanupDifference(t *testing.T) {
	p1 := &Plan{
		Execution: Execution{
			Steps:   []Step{{Node: "testNode"}},
			Cleanup: []CleanupStep{{Node: "cleanup1"}},
		},
	}
	p2 := &Plan{
		Execution: Execution{
			Steps:   []Step{{Node: "testNode"}},
			Cleanup: []CleanupStep{{Node: "cleanup2"}},
		},
	}

	fp1 := Fingerprint(p1)
	fp2 := Fingerprint(p2)
	assert.NotEqual(t, fp1, fp2, "different cleanup steps should produce different fingerprints")
}

func TestFingerprint_VerificationDifference(t *testing.T) {
	p1 := &Plan{
		Execution: Execution{
			Steps:        []Step{{Node: "testNode"}},
			Verification: []VerificationStep{{Node: "verify1"}},
		},
	}
	p2 := &Plan{
		Execution: Execution{
			Steps:        []Step{{Node: "testNode"}},
			Verification: []VerificationStep{{Node: "verify2"}},
		},
	}

	fp1 := Fingerprint(p1)
	fp2 := Fingerprint(p2)
	assert.NotEqual(t, fp1, fp2, "different verification steps should produce different fingerprints")
}

func TestFingerprint_EmptyExecution(t *testing.T) {
	p := &Plan{
		Execution: Execution{},
	}
	fp := Fingerprint(p)
	assert.NotEmpty(t, fp, "empty execution should still produce a fingerprint")
	assert.Len(t, fp, 64)
}

func TestFingerprint_Deterministic(t *testing.T) {
	p := &Plan{
		Execution: Execution{
			Steps: []Step{
				{Node: "a", Values: map[string]StepValue{"x": {Default: "1"}, "y": {Default: "2"}}},
			},
		},
	}

	fp1 := Fingerprint(p)
	fp2 := Fingerprint(p)
	fp3 := Fingerprint(p)
	assert.Equal(t, fp1, fp2)
	assert.Equal(t, fp2, fp3)
}
