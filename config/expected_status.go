package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gburgyan/aat/internal/grpcstatus"
	"github.com/gburgyan/aat/internal/httpstatus"
	"gopkg.in/yaml.v3"
)

// ExpectedStatus is a status a step is expected to fail with. It lives here,
// below plan, so an override's expectFailure reads the same forms a plan step's
// does; plan names it by alias. It is written as an HTTP status code, a gRPC
// status name, or a status class:
//
//	expectFailure:
//	  status: [404]
//	  status: [NOT_FOUND]
//	  status: [4xx]
//
// A class matches any status in it. A gRPC status matches the class of the
// HTTP status it maps to, so 4xx takes NOT_FOUND and INVALID_ARGUMENT alike.
//
// Both forms carry a code, so every comparison the engine makes works the same
// for either. A name also keeps itself, because several gRPC codes map to one
// HTTP status — INVALID_ARGUMENT, FAILED_PRECONDITION, and OUT_OF_RANGE are all
// 400 — and a negative test that named one should not pass on another.
type ExpectedStatus struct {
	// Code is the HTTP status, or the one the named gRPC code maps to.
	Code int
	// Name is the gRPC status name when the status was written as one, and
	// empty when it was written as a number.
	Name string
	// Class is the leading digit of a status class such as 4xx, and 0 for an
	// exact status. A class has no Code.
	Class int
}

// HTTPStatus returns an expected status written as an HTTP code.
func HTTPStatus(code int) ExpectedStatus { return ExpectedStatus{Code: code} }

// String renders the status the way it was written.
func (e ExpectedStatus) String() string {
	if e.Class != 0 {
		return fmt.Sprintf("%dxx", e.Class)
	}
	if e.Name != "" {
		return e.Name
	}
	return fmt.Sprintf("%d", e.Code)
}

// IsFailure reports whether the status denotes a failure. Because OK is the
// only gRPC code that maps below 400, this is one rule for both protocols.
func (e ExpectedStatus) IsFailure() bool { return e.Code >= 400 || e.Class >= 4 }

// Matches reports whether a response failed with this status. A gRPC response
// is compared by name when the expectation named one, so that codes sharing an
// HTTP status stay distinguishable.
func (e ExpectedStatus) Matches(status int, grpcName string) bool {
	if e.Class != 0 {
		return status/100 == e.Class
	}
	if e.Name != "" && grpcName != "" {
		return strings.EqualFold(e.Name, grpcName)
	}
	return e.Code == status
}

// ParseExpectedStatus reads a status written as a number, a gRPC name, or a
// status class such as 4xx.
func ParseExpectedStatus(v any) (ExpectedStatus, error) {
	if class, ok := httpstatus.Class(v); ok {
		return ExpectedStatus{Class: class}, nil
	}
	switch t := v.(type) {
	case string:
		code, ok := grpcstatus.CodeByName(t)
		if !ok {
			return ExpectedStatus{}, fmt.Errorf("unknown status %q: write an HTTP status code, a class such as 4xx, or one of %s",
				t, strings.Join(grpcstatus.Names(), ", "))
		}
		return ExpectedStatus{Code: grpcstatus.HTTPStatus(code), Name: grpcstatus.Name(code)}, nil
	case int:
		return ExpectedStatus{Code: t}, nil
	case int64:
		return ExpectedStatus{Code: int(t)}, nil
	case float64:
		return ExpectedStatus{Code: int(t)}, nil
	default:
		return ExpectedStatus{}, fmt.Errorf("a status is a code such as 404, a class such as 4xx, or a gRPC name such as NOT_FOUND, not %T", v)
	}
}

// UnmarshalYAML reads either form.
func (e *ExpectedStatus) UnmarshalYAML(unmarshal func(any) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	parsed, err := ParseExpectedStatus(raw)
	if err != nil {
		return &yaml.TypeError{Errors: []string{err.Error()}}
	}
	*e = parsed
	return nil
}

// MarshalYAML writes the status back the way it was written.
func (e ExpectedStatus) MarshalYAML() (any, error) {
	if e.Class != 0 {
		return e.String(), nil
	}
	if e.Name != "" {
		return e.Name, nil
	}
	return e.Code, nil
}

// UnmarshalJSON reads either form, so an archived plan round-trips.
//
// Unlike the YAML form it does not reject an unrecognised name. YAML is what a
// person writes, and a typo there should be caught; JSON is what AAT has
// already written, and an archive must stay readable whatever it holds — a
// redacted name among it. An unrecognised name is kept as written, with no
// code, so it can only ever fail to match.
func (e *ExpectedStatus) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseExpectedStatus(raw)
	if err != nil {
		if name, isString := raw.(string); isString {
			*e = ExpectedStatus{Name: name}
			return nil
		}
		return err
	}
	*e = parsed
	return nil
}

// MarshalJSON writes the status the way it was written.
func (e ExpectedStatus) MarshalJSON() ([]byte, error) {
	if e.Class != 0 {
		return json.Marshal(e.String())
	}
	if e.Name != "" {
		return json.Marshal(e.Name)
	}
	return json.Marshal(e.Code)
}

// ExpectedStatuses is a list of expected statuses.
type ExpectedStatuses []ExpectedStatus

// Codes returns the statuses as HTTP codes, for the places that report them
// as numbers. A class has no code and reads as 0.
func (s ExpectedStatuses) Codes() []int {
	codes := make([]int, len(s))
	for i, e := range s {
		codes[i] = e.Code
	}
	return codes
}

// Strings renders each status the way it was written.
func (s ExpectedStatuses) Strings() []string {
	out := make([]string, len(s))
	for i, e := range s {
		out[i] = e.String()
	}
	return out
}

// Matches reports whether a response failed with any of the statuses.
func (s ExpectedStatuses) Matches(status int, grpcName string) bool {
	for _, e := range s {
		if e.Matches(status, grpcName) {
			return true
		}
	}
	return false
}

// HTTPStatuses converts plain codes, for callers that still hold ints.
func HTTPStatuses(codes []int) ExpectedStatuses {
	if codes == nil {
		return nil
	}
	out := make(ExpectedStatuses, len(codes))
	for i, c := range codes {
		out[i] = HTTPStatus(c)
	}
	return out
}
