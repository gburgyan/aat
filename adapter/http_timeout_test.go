package adapter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPExecutor_DefaultTimeout(t *testing.T) {
	assert.Equal(t, DefaultRequestTimeout, NewHTTPExecutor("http://shop.example").Client.Timeout)
}

// TestHTTPExecutor_TimeoutNamesTheLimit checks that a request the client gives
// up on says so, naming the limit, and is still a timeout error.
func TestHTTPExecutor_TimeoutNamesTheLimit(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	e := NewHTTPExecutorWithClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond})
	_, err := e.Execute(context.Background(), &Request{Method: "GET", Path: "/orders"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no response within aat's 50ms request timeout")
	var netErr net.Error
	require.True(t, errors.As(err, &netErr), "still a net.Error: %v", err)
	assert.True(t, netErr.Timeout())
}

// TestHTTPExecutor_CancelledContextDoesNotBlameTheTimeout checks that a request
// stopped by its context, such as an interrupted run, doesn't name the limit.
func TestHTTPExecutor_CancelledContextDoesNotBlameTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	e := NewHTTPExecutorWithClient(srv.URL, &http.Client{Timeout: time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := e.Execute(ctx, &Request{Method: "GET", Path: "/orders"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "request timeout")
}
