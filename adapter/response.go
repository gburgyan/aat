package adapter

import "net/http"

// Response represents the result of an HTTP call executed by the HTTPExecutor.
type Response struct {
	StatusCode int
	Headers    http.Header // multi-value capable
	Body       []byte
}
