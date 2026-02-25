package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPExecutor_Execute(t *testing.T) {
	t.Run("GET success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/status", r.URL.Path)
			w.Header().Set("X-Custom", "value")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		exec := NewHTTPExecutor(srv.URL)
		resp, err := exec.Execute(context.Background(), &Request{
			Method: http.MethodGet,
			Path:   "/v2/status",
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "value", resp.Headers.Get("X-Custom"))
		assert.JSONEq(t, `{"status":"ok"}`, string(resp.Body))
	})

	t.Run("POST with JSON body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			body, _ := io.ReadAll(r.Body)
			assert.JSONEq(t, `{"origin":"JFK","destination":"LAX"}`, string(body))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"123"}`))
		}))
		defer srv.Close()

		exec := NewHTTPExecutor(srv.URL)
		resp, err := exec.Execute(context.Background(), &Request{
			Method:  http.MethodPost,
			Path:    "/v2/flights/search",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"origin":"JFK","destination":"LAX"}`),
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.JSONEq(t, `{"id":"123"}`, string(resp.Body))
	})

	t.Run("status codes", func(t *testing.T) {
		codes := []int{200, 400, 401, 404, 500, 503}
		for _, code := range codes {
			code := code
			t.Run(http.StatusText(code), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(code)
				}))
				defer srv.Close()

				exec := NewHTTPExecutor(srv.URL)
				resp, err := exec.Execute(context.Background(), &Request{
					Method: http.MethodGet,
					Path:   "/test",
				})

				require.NoError(t, err)
				assert.Equal(t, code, resp.StatusCode)
			})
		}
	})

	t.Run("request headers sent correctly", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		exec := NewHTTPExecutor(srv.URL)
		_, err := exec.Execute(context.Background(), &Request{
			Method: http.MethodGet,
			Path:   "/secure",
			Headers: map[string]string{
				"Authorization": "Bearer token123",
				"Accept":        "application/json",
			},
		})
		require.NoError(t, err)
	})

	t.Run("response multi-value headers preserved", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Set-Cookie", "a=1")
			w.Header().Add("Set-Cookie", "b=2")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		exec := NewHTTPExecutor(srv.URL)
		resp, err := exec.Execute(context.Background(), &Request{
			Method: http.MethodGet,
			Path:   "/cookies",
		})

		require.NoError(t, err)
		cookies := resp.Headers.Values("Set-Cookie")
		assert.Len(t, cookies, 2)
		assert.Contains(t, cookies, "a=1")
		assert.Contains(t, cookies, "b=2")
	})

	t.Run("context cancellation returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		exec := NewHTTPExecutor(srv.URL)
		_, err := exec.Execute(ctx, &Request{
			Method: http.MethodGet,
			Path:   "/slow",
		})
		assert.Error(t, err)
	})

	t.Run("nil body for GET", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Empty(t, body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		exec := NewHTTPExecutor(srv.URL)
		resp, err := exec.Execute(context.Background(), &Request{
			Method: http.MethodGet,
			Path:   "/empty",
			Body:   nil,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestHTTPExecutor_URLJoining(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		path     string
		wantPath string
	}{
		{
			name:     "base with trailing slash, path with leading slash",
			baseURL:  "/",
			path:     "/v2/search",
			wantPath: "/v2/search",
		},
		{
			name:     "base without trailing slash, path with leading slash",
			baseURL:  "",
			path:     "/v2/search",
			wantPath: "/v2/search",
		},
		{
			name:     "path with query params",
			baseURL:  "",
			path:     "/v2/search?limit=10",
			wantPath: "/v2/search",
		},
		{
			name:     "path without leading slash",
			baseURL:  "",
			path:     "v2/search",
			wantPath: "/v2/search",
		},
		{
			name:     "base with path prefix, path with leading slash",
			baseURL:  "/v2",
			path:     "/pet",
			wantPath: "/v2/pet",
		},
		{
			name:     "base with path prefix and trailing slash",
			baseURL:  "/v2/",
			path:     "/pet",
			wantPath: "/v2/pet",
		},
		{
			name:     "base with deep path prefix",
			baseURL:  "/api/v3",
			path:     "/pet/findByStatus",
			wantPath: "/api/v3/pet/findByStatus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedPath string
			var receivedQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				receivedQuery = r.URL.RawQuery
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			exec := NewHTTPExecutor(srv.URL + tt.baseURL)
			_, err := exec.Execute(context.Background(), &Request{
				Method: http.MethodGet,
				Path:   tt.path,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, receivedPath)

			if tt.path == "/v2/search?limit=10" {
				assert.Equal(t, "limit=10", receivedQuery)
			}
		})
	}
}

func TestNewHTTPExecutor(t *testing.T) {
	exec := NewHTTPExecutor("https://api.example.com")
	assert.Equal(t, "https://api.example.com", exec.BaseURL)
	assert.NotNil(t, exec.Client)
}

func TestNewHTTPExecutorWithClient(t *testing.T) {
	client := &http.Client{}
	exec := NewHTTPExecutorWithClient("https://api.example.com", client)
	assert.Equal(t, "https://api.example.com", exec.BaseURL)
	assert.Same(t, client, exec.Client)
}
