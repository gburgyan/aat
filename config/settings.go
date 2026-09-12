package config

import (
	"fmt"
	"time"
)

// RequestInterval returns settings.minRequestInterval as a duration: zero when
// it is empty, and an error unless it is a non-negative duration with a unit,
// such as 250ms or 1s.
func (s RuntimeSettings) RequestInterval() (time.Duration, error) {
	if s.MinRequestInterval == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s.MinRequestInterval)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q (use a unit, such as 250ms or 1s)", s.MinRequestInterval)
	}
	return d, nil
}
