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
