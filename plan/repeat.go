package plan

import (
	"fmt"
	"slices"
	"time"
)

// RepeatConfig sends a step's request again and again until a condition over
// its response holds, as when polling an asynchronous job. Every request
// resends the inputs the first resolved.
type RepeatConfig struct {
	// Until is a predicate over each response's outputs. The step stops sending
	// requests as soon as it holds.
	Until string `yaml:"until" json:"until"`
	// Collect names outputs gathered across the responses: the items of a list
	// output are appended in order, and a number output's values are added.
	Collect []string `yaml:"collect,omitempty" json:"collect,omitempty"`
	// Interval is the wait between requests, such as 500ms or 2s; empty means
	// 1s. A response's Retry-After header lengthens it, up to 60s.
	Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`
	// Max caps the requests the step sends; 0 means DefaultRepeatMax.
	Max int `yaml:"max,omitempty" json:"max,omitempty"`
	// Timeout caps the time the step spends repeating, such as 3m; empty means
	// no cap but Max.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// Defaults for a repeat block that leaves max or interval out, and the most
// requests one may send.
const (
	DefaultRepeatMax      = 50
	DefaultRepeatInterval = time.Second
	MaxRepeatRequests     = 1000
)

// MaxRequests returns the most requests the step sends.
func (r *RepeatConfig) MaxRequests() int {
	if r.Max <= 0 {
		return DefaultRepeatMax
	}
	return r.Max
}

// IntervalDuration returns the wait between requests.
func (r *RepeatConfig) IntervalDuration() (time.Duration, error) {
	if r.Interval == "" {
		return DefaultRepeatInterval, nil
	}
	return parseRepeatDuration("interval", r.Interval)
}

// TimeoutDuration returns the cap on the time the step spends repeating, or 0
// when there is none.
func (r *RepeatConfig) TimeoutDuration() (time.Duration, error) {
	if r.Timeout == "" {
		return 0, nil
	}
	return parseRepeatDuration("timeout", r.Timeout)
}

// Clone returns a copy of r that shares nothing with it, or nil for nil.
func (r *RepeatConfig) Clone() *RepeatConfig {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Collect = slices.Clone(r.Collect)
	return &cp
}

func parseRepeatDuration(field, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("repeat.%s %q is not a duration (use a unit, such as 500ms, 2s, or 3m)", field, s)
	}
	return d, nil
}
