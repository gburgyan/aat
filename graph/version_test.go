package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Version
		wantErr bool
	}{
		{name: "1.0.0", input: "1.0.0", want: Version{1, 0, 0}},
		{name: "0.1.0", input: "0.1.0", want: Version{0, 1, 0}},
		{name: "10.20.30", input: "10.20.30", want: Version{10, 20, 30}},
		{name: "0.0.0", input: "0.0.0", want: Version{0, 0, 0}},

		// Invalid
		{name: "two components", input: "1.0", wantErr: true},
		{name: "alpha", input: "abc", wantErr: true},
		{name: "four components", input: "1.0.0.0", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "leading v", input: "v1.0.0", wantErr: true},
		{name: "with pre-release", input: "1.0.0-beta", wantErr: true},
		{name: "with build", input: "1.0.0+build", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersion_String(t *testing.T) {
	assert.Equal(t, "1.2.3", Version{1, 2, 3}.String())
	assert.Equal(t, "0.0.0", Version{0, 0, 0}.String())
}

func TestCheckCompatibility(t *testing.T) {
	tests := []struct {
		name         string
		planVersion  Version
		graphVersion Version
		want         Compatibility
	}{
		{
			name:         "same version",
			planVersion:  Version{1, 0, 0},
			graphVersion: Version{1, 0, 0},
			want:         VersionCompatible,
		},
		{
			name:         "graph higher minor",
			planVersion:  Version{1, 0, 0},
			graphVersion: Version{1, 2, 0},
			want:         VersionCompatible,
		},
		{
			name:         "graph higher patch",
			planVersion:  Version{1, 1, 0},
			graphVersion: Version{1, 1, 5},
			want:         VersionCompatible,
		},
		{
			name:         "plan higher minor (drift)",
			planVersion:  Version{1, 3, 0},
			graphVersion: Version{1, 1, 0},
			want:         VersionMinorDrift,
		},
		{
			name:         "different major (plan higher)",
			planVersion:  Version{2, 0, 0},
			graphVersion: Version{1, 5, 0},
			want:         VersionIncompatible,
		},
		{
			name:         "different major (graph higher)",
			planVersion:  Version{1, 0, 0},
			graphVersion: Version{2, 0, 0},
			want:         VersionIncompatible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckCompatibility(tt.planVersion, tt.graphVersion)
			assert.Equal(t, tt.want, got)
		})
	}
}
