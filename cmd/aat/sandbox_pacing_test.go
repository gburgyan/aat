package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// arrivalLog records when requests reach the sandbox. OAuth2 token requests
// are left out, because AAT does not pace them.
type arrivalLog struct {
	mu    sync.Mutex
	times []time.Time
}

func (a *arrivalLog) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			a.mu.Lock()
			a.times = append(a.times, time.Now())
			a.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

// minGap returns the shortest time between two consecutive arrivals.
func (a *arrivalLog) minGap(t *testing.T) time.Duration {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	require.GreaterOrEqual(t, len(a.times), 2)
	sorted := append([]time.Time(nil), a.times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	gap := sorted[1].Sub(sorted[0])
	for i := 2; i < len(sorted); i++ {
		gap = min(gap, sorted[i].Sub(sorted[i-1]))
	}
	return gap
}

// TestShopExample_RequestPacing runs the shop's plans two at a time with
// settings.minRequestInterval set. The sandbox sees the requests of both runs
// spaced apart, because the plans of a batch share one pacer.
func TestShopExample_RequestPacing(t *testing.T) {
	if testing.Short() {
		t.Skip("shop example end-to-end test skipped in -short mode")
	}
	const interval = 50 * time.Millisecond

	var arrivals arrivalLog
	p := newShopProjectWith(t, arrivals.record)
	env, err := os.ReadFile(p.m.EnvPath)
	require.NoError(t, err)
	paced := strings.Replace(string(env), "shared:\n", "shared:\n  settings:\n    minRequestInterval: 50ms\n", 1)
	require.NotEqual(t, string(env), paced, "the shop env.yaml has a shared block")
	require.NoError(t, os.WriteFile(p.m.EnvPath, []byte(paced), 0o600))

	res := batchCommand(context.Background(), &batchArgs{
		runArgs:  p.runArgs(t, "us"),
		PlanDirs: []string{filepath.Join(p.dir, "plans")},
		Parallel: 2,
	}, io.Discard)
	require.NoError(t, res.err)
	require.NotNil(t, res.summary)
	assert.Equal(t, "passed", res.summary.Outcome, failedRuns(res.summary))
	assert.Equal(t, 3, res.summary.Summary.TotalPlans)

	// Unpaced, two runs side by side send requests within a millisecond or two
	// of each other. Half an interval of slack absorbs scheduling delays.
	assert.GreaterOrEqual(t, arrivals.minGap(t), interval/2)
}
