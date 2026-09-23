package engine

import (
	"hash/fnv"
	"math/rand/v2"
	"strconv"
	"sync"
)

// WithSeed sets the seed that pool picks and random selections are drawn
// from, so a run can be replayed with the same choices. Without it, each Run
// picks a seed of its own and reports it in RunResult.Seed.
//
// {{uuid}} and {{random N}} do not use the seed: they make values unique, and
// replaying them would collide with the resources the first run created.
func (e *Engine) WithSeed(seed uint64) *Engine {
	e.seed = seed
	e.seedSet = true
	return e
}

// NewRunSeed returns a random seed for a run that was given none. It keeps to
// 53 bits so the number survives JSON readers that hold it as a float, such
// as the web UI.
func NewRunSeed() uint64 {
	return rand.Uint64() >> 11
}

// stepDraws hands each resolution of a step its own random source, derived
// from the run's seed, the step's ID, and how many times the step has resolved
// before. Steps that run in parallel therefore draw the same values whatever
// order they are scheduled in, and a repeated step draws afresh each time.
type stepDraws struct {
	seed uint64
	mu   sync.Mutex
	n    map[string]int
}

func newStepDraws(seed uint64) *stepDraws {
	return &stepDraws{seed: seed, n: map[string]int{}}
}

// next returns the random source for the next resolution of stepID.
func (d *stepDraws) next(stepID string) *rand.Rand {
	d.mu.Lock()
	n := d.n[stepID]
	d.n[stepID] = n + 1
	d.mu.Unlock()

	h := fnv.New64a()
	_, _ = h.Write([]byte(stepID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.Itoa(n)))
	return rand.New(rand.NewPCG(d.seed, h.Sum64()))
}

// DrewRandomly reports whether the run made a choice its seed decides: a pick
// from a pool of more than one value, or a random selection. Only then is the
// seed worth reporting.
func (r *RunResult) DrewRandomly() bool {
	if r == nil {
		return false
	}
	for _, s := range r.Steps {
		for _, v := range s.Resolutions {
			if v.Source == "fallback_pool" && v.PoolSize > 1 {
				return true
			}
		}
		for _, d := range s.Selections {
			if d.Strategy == "random" {
				return true
			}
		}
	}
	return false
}
