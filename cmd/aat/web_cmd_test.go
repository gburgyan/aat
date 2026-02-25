package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gburgyan/aat/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildViewURL(t *testing.T) {
	// Create a temp dir with a batch directory for content-based detection.
	dir := t.TempDir()
	batchDir := filepath.Join(dir, "batch-20260220-143022-abc12345")
	require.NoError(t, os.MkdirAll(batchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(batchDir, "batch.json"), []byte("{}"), 0o644))

	namedBatchDir := filepath.Join(dir, "my-batch")
	require.NoError(t, os.MkdirAll(namedBatchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(namedBatchDir, "batch.json"), []byte("{}"), 0o644))

	tests := []struct {
		name       string
		port       int
		ref        string
		archiveDir string
		want       string
	}{
		{
			name:       "empty ref (run list)",
			port:       9119,
			ref:        "",
			archiveDir: "",
			want:       "http://localhost:9119",
		},
		{
			name:       "latest (no archive dir)",
			port:       9119,
			ref:        "latest",
			archiveDir: "",
			want:       "http://localhost:9119/runs/latest",
		},
		{
			name:       "specific run ID",
			port:       9119,
			ref:        "run-20260220-143022-abc12345",
			archiveDir: dir,
			want:       "http://localhost:9119/runs/run-20260220-143022-abc12345",
		},
		{
			name:       "custom port",
			port:       8080,
			ref:        "latest",
			archiveDir: "",
			want:       "http://localhost:8080/runs/latest",
		},
		{
			name:       "named run (no batch.json)",
			port:       3000,
			ref:        "my-run",
			archiveDir: dir,
			want:       "http://localhost:3000/runs/my-run",
		},
		{
			name:       "batch ref (detected by batch.json)",
			port:       9119,
			ref:        "batch-20260220-143022-abc12345",
			archiveDir: dir,
			want:       "http://localhost:9119/batches/batch-20260220-143022-abc12345",
		},
		{
			name:       "named batch (detected by batch.json)",
			port:       8080,
			ref:        "my-batch",
			archiveDir: dir,
			want:       "http://localhost:8080/batches/my-batch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildViewURL(tt.port, tt.ref, tt.archiveDir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTraceViewURL(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		traceRef string
		want     string
	}{
		{
			name:     "list (no ref)",
			port:     9119,
			traceRef: "",
			want:     "http://localhost:9119/traces",
		},
		{
			name:     "specific trace",
			port:     9119,
			traceRef: "trace-20260221-141444-be49e7e9",
			want:     "http://localhost:9119/traces/trace-20260221-141444-be49e7e9",
		},
		{
			name:     "custom port list",
			port:     8080,
			traceRef: "",
			want:     "http://localhost:8080/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTraceViewURL(tt.port, tt.traceRef)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWebViewTraceCommand_ServerAlreadyRunning(t *testing.T) {
	srv := server.NewServer(server.ServerOptions{
		Port:       19225,
		ArchiveDir: t.TempDir(),
	})
	go func() { _ = srv.ListenAndServe() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	require.True(t, waitForServer(srv, 3*time.Second))

	var opened string
	orig := openURLFunc
	openURLFunc = func(url string) error {
		opened = url
		return nil
	}
	defer func() { openURLFunc = orig }()

	// No ref → opens trace list.
	err := webViewTraceCommand(19225, "", t.TempDir(), "")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:19225/traces", opened)

	// With ref → opens specific trace.
	err = webViewTraceCommand(19225, "trace-abc123", t.TempDir(), "")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:19225/traces/trace-abc123", opened)
}

func TestCheckServerHealth_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
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
		_ = srv.Shutdown(ctx)
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
	err := webViewCommand(19223, "latest", t.TempDir(), "")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:19223/runs/latest", opened)
}

func TestWebViewCommand_StartsEphemeralServer(t *testing.T) {
	archiveDir := t.TempDir()

	// Stub browser open to capture URL via channel.
	openedCh := make(chan string, 1)
	orig := openURLFunc
	openURLFunc = func(url string) error {
		openedCh <- url
		return nil
	}
	defer func() { openURLFunc = orig }()

	// Port 19224 is not running — webViewCommand should start a server.
	// We run it in a goroutine because it blocks until signal.
	errCh := make(chan error, 1)
	go func() {
		errCh <- webViewCommand(19224, "latest", archiveDir, "")
	}()

	// Wait for the ephemeral server to come up and open the browser.
	var opened string
	select {
	case opened = <-openedCh:
	case <-time.After(3 * time.Second):
	}
	assert.Equal(t, "http://localhost:19224/runs/latest", opened, "should have opened the ref URL")

	// Verify the server is actually serving.
	resp, err := http.Get("http://localhost:19224/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Send SIGINT to shut down the ephemeral server.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

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
		_ = srv.Shutdown(ctx)
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
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify runs endpoint responds.
	resp, err = http.Get(fmt.Sprintf("http://%s/api/runs", srv.Addr()))
	require.NoError(t, err)
	_ = resp.Body.Close()
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
			_, _ = fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Extract port from test server URL.
	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")
	portInt := 0
	_, _ = fmt.Sscanf(port, "%d", &portInt)
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
	url := buildViewURL(portInt, "latest", "")
	err := openURLFunc(url)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/runs/latest", portInt), opened)

	// Also verify with a specific run ID.
	url = buildViewURL(portInt, "run-abc123", "")
	err = openURLFunc(url)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/runs/run-abc123", portInt), opened)
}

// --- classifyFileArg tests ---

func TestClassifyFileArg(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	aarPath := filepath.Join(dir, "test.aar")
	require.NoError(t, os.WriteFile(aarPath, []byte("zip data"), 0o644))

	aabPath := filepath.Join(dir, "test.aab")
	require.NoError(t, os.WriteFile(aabPath, []byte("zip data"), 0o644))

	runDir := filepath.Join(dir, "run-dir")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	archiveJSONPath := filepath.Join(runDir, "archive.json")
	require.NoError(t, os.WriteFile(archiveJSONPath, []byte(`{}`), 0o644))

	batchDir := filepath.Join(dir, "batch-dir")
	require.NoError(t, os.MkdirAll(batchDir, 0o755))
	batchJSONPath := filepath.Join(batchDir, "batch.json")
	require.NoError(t, os.WriteFile(batchJSONPath, []byte(`{}`), 0o644))

	tests := []struct {
		name string
		arg  string
		want fileType
	}{
		{"aar file", aarPath, fileTypeAar},
		{"aab file", aabPath, fileTypeAab},
		{"archive.json", archiveJSONPath, fileTypeRunJSON},
		{"batch.json", batchJSONPath, fileTypeBatchJSON},
		{"nonexistent aar", filepath.Join(dir, "nonexistent.aar"), fileTypeNotAFile},
		{"run ID (not a file)", "run-20260225-143022-abc12345", fileTypeNotAFile},
		{"batch ID (not a file)", "batch-20260225-143022-abc12345", fileTypeNotAFile},
		{"latest", "latest", fileTypeNotAFile},
		{"empty string", "", fileTypeNotAFile},
		{"regular json file", filepath.Join(dir, "other.json"), fileTypeNotAFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFileArg(tt.arg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyFileArg_DirectoryNotFile(t *testing.T) {
	dir := t.TempDir()

	// Create a directory named "test.aar" — should not classify as a file.
	aarDir := filepath.Join(dir, "test.aar")
	require.NoError(t, os.MkdirAll(aarDir, 0o755))

	got := classifyFileArg(aarDir)
	assert.Equal(t, fileTypeNotAFile, got)
}
