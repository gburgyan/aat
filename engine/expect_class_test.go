package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/plan"
)

func TestExpectFailure_StatusClass(t *testing.T) {
	tests := []struct {
		status int
		want   Outcome
	}{
		{http.StatusUnprocessableEntity, OutcomePassed},
		{http.StatusConflict, OutcomePassed},
		{http.StatusInternalServerError, OutcomeFailed},
		{http.StatusCreated, OutcomeFailed},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			p := orderPlan(plan.StepValue{Default: "r-1"})
			p.Execution.Steps[0].ExpectFailure = &plan.ExpectFailure{Status: plan.ExpectedStatuses{{Class: 4}}}
			result := buildOrderEngine(t, server.URL).Run(context.Background(), p)
			assert.Equal(t, tt.want, result.Outcome, "run error: %v", result.Error)
			require.Len(t, result.Steps, 1)
			require.NotNil(t, result.Steps[0].ExpectFailure)
			assert.Equal(t, tt.want == OutcomePassed, result.Steps[0].ExpectFailure.Passed)
		})
	}
}
