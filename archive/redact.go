package archive

import "strings"

// sensitiveHeaders lists header names whose values should be redacted in archives.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"x-auth-token":        true,
	"cookie":              true,
	"set-cookie":          true,
	"proxy-authorization": true,
}

// RedactHeaders returns a copy of headers with sensitive values replaced by [REDACTED].
// Matching is case-insensitive.
func RedactHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if sensitiveHeaders[strings.ToLower(k)] {
			result[k] = "[REDACTED]"
		} else {
			result[k] = v
		}
	}
	return result
}

// RedactValue redacts secret values from an arbitrary value.
// If val is a string that exactly matches a secret, it returns "[REDACTED]".
// If val is a string containing a secret as a substring, all occurrences are replaced.
// Non-string types and nil/empty secrets pass through unchanged.
func RedactValue(val any, secrets map[string]bool) any {
	if len(secrets) == 0 {
		return val
	}
	s, ok := val.(string)
	if !ok {
		return val
	}
	for secret := range secrets {
		if s == secret {
			return "[REDACTED]"
		}
		if strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return s
}

// RedactSlice applies RedactValue to each element of a slice.
func RedactSlice(vals []any, secrets map[string]bool) []any {
	if len(secrets) == 0 || len(vals) == 0 {
		return vals
	}
	result := make([]any, len(vals))
	for i, v := range vals {
		result[i] = RedactValue(v, secrets)
	}
	return result
}

// RedactMap applies RedactValue to each value in a map.
// Keys are converted to string via fmt.Sprintf if not already strings.
func RedactMap(m map[string]any, secrets map[string]bool) map[string]any {
	if len(secrets) == 0 || len(m) == 0 {
		return m
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = RedactValue(v, secrets)
	}
	return result
}
