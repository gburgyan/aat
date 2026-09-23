package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolRefValues(t *testing.T) {
	kb := &KnowledgeBase{ValuePools: map[string]*ValuePool{
		"airportCodes": {Values: []string{"JFK"}, Groups: map[string][]string{"eu": {"CDG", "LHR"}, "us": {"ORD"}}},
		"currencies":   {Values: []string{"USD", "EUR"}},
	}}

	tests := []struct {
		ref     string
		want    []string
		wantErr string
	}{
		{ref: "currencies", want: []string{"USD", "EUR"}},
		{ref: "airportCodes", want: []string{"JFK", "CDG", "LHR", "ORD"}},
		{ref: "airportCodes.eu", want: []string{"CDG", "LHR"}},
		{ref: "airportCode", wantErr: `no value pool "airportCode" (did you mean "airportCodes"?)`},
		{ref: "airportCodes.asia", wantErr: `has no group "asia" (it has eu, us)`},
		{ref: "currencies.eu", wantErr: `value pool "currencies" has no groups`},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got, err := kb.PoolRefValues(tt.ref)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPoolRefValues_NoDomain(t *testing.T) {
	var kb *KnowledgeBase
	_, err := kb.PoolRefValues("airportCodes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a domain file")
}
