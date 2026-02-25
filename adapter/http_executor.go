package adapter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPExecutor sends adapter-built requests over HTTP. It owns the base URL
// and HTTP client; adapters produce relative paths.
type HTTPExecutor struct {
	Client  *http.Client
	BaseURL string
}

// NewHTTPExecutor creates an executor with a default 30-second timeout client.
func NewHTTPExecutor(baseURL string) *HTTPExecutor {
	return &HTTPExecutor{
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		BaseURL: baseURL,
	}
}

// NewHTTPExecutorWithClient creates an executor with the provided HTTP client.
func NewHTTPExecutorWithClient(baseURL string, client *http.Client) *HTTPExecutor {
	return &HTTPExecutor{
		Client:  client,
		BaseURL: baseURL,
	}
}

// Execute sends the request and returns the response. It joins BaseURL with
// the request's relative Path, copies headers, and respects the context for
// cancellation and timeouts.
func (e *HTTPExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	fullURL, err := joinURL(e.BaseURL, req.Path)
	if err != nil {
		return nil, fmt.Errorf("building URL: %w", err)
	}

	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = strings.NewReader(string(req.Body))
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := e.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}

// joinURL combines a base URL with a relative path, preserving query parameters
// from the path. Unlike url.ResolveReference, this always appends the path to
// the base URL's existing path (e.g. base="/v2" + path="/pet" → "/v2/pet").
func joinURL(base, relPath string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parsing base URL %q: %w", base, err)
	}

	pathURL, err := url.Parse(relPath)
	if err != nil {
		return "", fmt.Errorf("parsing path %q: %w", relPath, err)
	}

	// Concatenate paths: strip trailing slash from base, ensure leading slash on path.
	bp := strings.TrimRight(baseURL.Path, "/")
	rp := pathURL.Path
	if rp != "" && !strings.HasPrefix(rp, "/") {
		rp = "/" + rp
	}
	baseURL.Path = bp + rp

	// Merge query parameters: path's query wins if present, otherwise keep base's.
	if pathURL.RawQuery != "" {
		baseURL.RawQuery = pathURL.RawQuery
	}

	return baseURL.String(), nil
}
