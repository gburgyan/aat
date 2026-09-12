package config

import "errors"

// ErrUnusableVars matches (errors.Is) an error about vars set from outside an
// environment file, such as --var flags, that the file cannot use: a key it
// never declares or references, or any var for a single-environment file. The
// problem is in how aat was invoked, not in the file.
var ErrUnusableVars = errors.New("vars the environment file cannot use")

// unusableVarsError is an error message that matches ErrUnusableVars.
type unusableVarsError string

func (e unusableVarsError) Error() string { return string(e) }

// Is reports whether target is ErrUnusableVars.
func (e unusableVarsError) Is(target error) bool { return target == ErrUnusableVars }
