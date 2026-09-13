package engine

import "errors"

// ErrOutputMissing is wrapped by RunState.GetOutput's error when a step stored
// no value for the output, such as an optional output its response didn't
// have. An optional input that takes from: such an output is left out.
var ErrOutputMissing = errors.New("output missing")

// outputMissingError reports a missing output with its own message, and
// ErrOutputMissing underneath.
type outputMissingError struct{ msg string }

func (e outputMissingError) Error() string { return e.msg }

func (e outputMissingError) Unwrap() error { return ErrOutputMissing }
