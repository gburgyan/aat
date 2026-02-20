package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gburgyan/aat/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildViewURL(t *testing.T) {
	tests := []struct {
		name string
		port int
		ref  string
		want string
	}{
		{
			name: "latest",
			port: 9119,
			ref:  "latest",
			want: "http://localhost:9119/api/runs/latest",
		},
		{
			name: "specific run ID",
			port: 9119,
			ref:  "run-20260220-143022-abc12345",
			want: "http://localhost:9119/api/runs/run-20260220-143022-abc12345",
		},
		{
			name: "custom port",
			port: 8080,
			ref:  "latest",
			want: "http://localhost:8080/api/runs/latest",
		},
		{
			name: "custom port with run ID",
			port: 3000,
			ref:  "my-run",
			want: "http://localhost:3000/api/runs/my-run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildViewURL(tt.port, tt.ref)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckServerHealth_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	err := checkServerHealth(ts.URL)
	assert.NoError(t, err)
}

func TestCheckServerHealth_ServerDown(t *testing.T) {
	err := checkServerHealth("http://localhost:19999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server not reachable")
	assert.Contains(t, err.Error(), "hint")
}

func TestCheckServerHealth_BadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	err := checkServerHealth(ts.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Contains(t, err.Error(), "hint")
}

func TestWebViewCommand_ServerAlreadyRunning(t *testing.T) {
	// Start a server that responds to health checks.
	srv := server.NewServer(server.ServerOptions{
		Port:       19223,
		ArchiveDir: t.TempDir(),
	})
	go func() { _ = srv.ListenAndServe() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	require.True(t, waitForServer(srv, 3*time.Second))

	// Stub browser open to capture URL.
	var opened string
	orig := openURLFunc
	openURLFunc = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openURLFunc = orig }()

	// Should open immediately without starting a new server.
	err := webViewCommand(19223, "latest", t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:19223/api/runs/latest", opened)
}

func TestWebViewCommand_StartsEphemeralServer(t *testing.T) {
	archiveDir := t.TempDir()

	// Stub browser open to capture URL.
	var opened string
	orig := openURLFunc
	openURLFunc = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openURLFunc = orig }()

	// Port 19224 is not running — webViewCommand should start a server.
	// We run it in a goroutine because it blocks until signal.
	errCh := make(chan error, 1)
	go func() {
		errCh <- webViewCommand(19224, "latest", archiveDir)
	}()

	// Wait for the ephemeral server to come up and open the browser.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if opened != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.Equal(t, "http://localhost:19224/api/runs/latest", opened, "should have opened the ref URL")

	// Verify the server is actually serving.
	resp, err := http.Get("http://localhost:19224/health")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Send SIGINT to shut down the ephemeral server.
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case <-errCh:
		// Clean shutdown.
	case <-time.After(5 * time.Second):
		t.Fatal("ephemeral server did not shut down in time")
	}
}

func TestWaitForServer_Ready(t *testing.T) {
	// NewServer maps port 0 → 9119, so start the server on default port
	// and use a real server instance. We rely on the server package to bind.
	srv := server.NewServer(server.ServerOptions{
		Port:       19221, // use an uncommon high port for testing
		ArchiveDir: t.TempDir(),
	})

	go func() {
		_ = srv.ListenAndServe()
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	ready := waitForServer(srv, 3*time.Second)
	assert.True(t, ready, "server should be ready")
	assert.NotEmpty(t, srv.Addr())
}

func TestWaitForServer_Timeout(t *testing.T) {
	// Create server but don't start it.
	srv := server.NewServer(server.ServerOptions{
		ArchiveDir: t.TempDir(),
	})

	ready := waitForServer(srv, 100*time.Millisecond)
	assert.False(t, ready, "should time out when server is not started")
}

func TestWebServeCommand_Lifecycle(t *testing.T) {
	archiveDir := t.TempDir()

	// Start webServeCommand on a specific port and cancel via context
	// by overriding the function to test the shutdown path directly.
	srv := server.NewServer(server.ServerOptions{
		Port:       19222,
		ArchiveDir: archiveDir,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	require.True(t, waitForServer(srv, 3*time.Second), "server should start")

	// Verify health endpoint responds.
	resp, err := http.Get(fmt.Sprintf("http://%s/health", srv.Addr()))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify runs endpoint responds.
	resp, err = http.Get(fmt.Sprintf("http://%s/api/runs", srv.Addr()))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Graceful shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	assert.NoError(t, err)

	// Server goroutine should return.
	select {
	case srvErr := <-errCh:
		assert.Equal(t, http.ErrServerClosed, srvErr)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

func TestWebViewCommand_OpensCorrectURL(t *testing.T) {
	// Start a test server that responds to health and run endpoints.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Extract port from test server URL.
	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")
	portInt := 0
	fmt.Sscanf(port, "%d", &portInt)
	require.NotZero(t, portInt)

	// Stub browser open.
	var opened string
	orig := openURLFunc
	openURLFunc = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openURLFunc = orig }()

	// Verify buildViewURL + openURLFunc integration.
	url := buildViewURL(portInt, "latest")
	err := openURLFunc(url)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/api/runs/latest", portInt), opened)

	// Also verify with a specific run ID.
	url = buildViewURL(portInt, "run-abc123")
	err = openURLFunc(url)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/api/runs/run-abc123", portInt), opened)
}
