package adapter

// Request represents an outbound HTTP request built by an adapter.
// Adapters produce relative paths; the HTTPExecutor joins BaseURL + Path.
type Request struct {
	Method  string            // GET, POST, PUT, DELETE, PATCH
	Path    string            // relative URL path (e.g., "/v2/flights/search")
	Headers map[string]string // single-value headers set by adapter
	Body    []byte            // nil for bodiless methods
}
