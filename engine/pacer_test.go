package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gburgyan/aat/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertPaced checks that request starts, in any order, span at least one
// interval per gap between them. Half an interval of slack absorbs the time
// between a start and the moment it was recorded.
func assertPaced(t *testing.T, starts []time.Time, interval time.Duration) {
	t.Helper()
	require.GreaterOrEqual(t, len(starts), 2)
	sorted := append([]time.Time(nil), starts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	span := sorted[len(sorted)-1].Sub(sorted[0])
	want := time.Duration(len(sorted)-1)*interval - interval/2
	assert.GreaterOrEqual(t, span, want, "%d starts paced at %s span %s", len(sorted), interval, span)
}

func TestNewPacer_NilWithoutInterval(t *testing.T) {
	assert.Nil(t, NewPacer(0))
	assert.Nil(t, NewPacer(-time.Second))
	assert.NotNil(t, NewPacer(time.Millisecond))
}

func TestPacer_NilIsNoop(t *testing.T) {
	var p *Pacer
	start := time.Now()
	for i := 0; i < 3; i++ {
		require.NoError(t, p.Wait(context.Background()))
	}
	assert.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestPacer_SpacesSequentialCalls(t *testing.T) {
	const interval = 20 * time.Millisecond
	p := NewPacer(interval)

	first := time.Now()
	require.NoError(t, p.Wait(context.Background()))
	assert.Less(t, time.Since(first), interval, "the first request does not wait")

	starts := []time.Time{first}
	for i := 0; i < 3; i++ {
		require.NoError(t, p.Wait(context.Background()))
		starts = append(starts, time.Now())
	}
	assertPaced(t, starts, interval)
}

func TestPacer_SharedAcrossGoroutines(t *testing.T) {
	const interval = 20 * time.Millisecond
	p := NewPacer(interval)

	var mu sync.Mutex
	var starts []time.Time
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, p.Wait(context.Background()))
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
		}()
	}
	wg.Wait()
	assertPaced(t, starts, interval)
}

func TestPacer_WaitHonorsContext(t *testing.T) {
	p := NewPacer(time.Hour)
	require.NoError(t, p.Wait(context.Background()), "the first slot is free")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.Wait(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "a cancelled wait returns at once")
}

// TestEngine_PacerCoversStepsVerificationAndCleanup checks that the engine
// paces every request it sends, not only main steps.
func TestEngine_PacerCoversStepsVerificationAndCleanup(t *testing.T) {
	const interval = 30 * time.Millisecond
	var mu sync.Mutex
	var paths []string
	var starts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		starts = append(starts, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	eng := lifecycleEngine(t, srv.URL).WithPacer(NewPacer(interval))
	result := eng.Run(context.Background(), lifecyclePlan(
		[]plan.CleanupStep{{Node: "cancelThing"}},
		[]plan.VerificationStep{{Node: "getThing"}},
	))
	require.Equal(t, OutcomePassed, result.Outcome, "%v", result.Error)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"/create", "/get", "/cancel", "/delete"}, paths,
		"a main step, a verification step, a plan cleanup step, and a graph cleanup pairing")
	assertPaced(t, starts, interval)
}
