package adapter

import (
	"net/http"
	"strings"
)

// headerValues returns the values of the response header name, matched in any
// case, even in a header map whose keys aren't in canonical form.
func headerValues(h http.Header, name string) []string {
	if values := h.Values(name); len(values) > 0 {
		return values
	}
	for key, values := range h {
		if strings.EqualFold(key, name) {
			return values
		}
	}
	return nil
}
