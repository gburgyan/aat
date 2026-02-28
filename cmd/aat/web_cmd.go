package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/server"
	"github.com/spf13/cobra"
)

// openURLFunc is the function used to open a URL in the browser.
// Replaced in tests for capturing.
var openURLFunc = openURL

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the AAT web UI server",
	Long:  "Start a local web server for browsing run archives and test results.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		port, _ := cmd.Flags().GetInt("port")
		openFlag, _ := cmd.Flags().GetBool("open")
		devMode, _ := cmd.Flags().GetBool("dev")

		outputDir := "_output/runs"
		if cmd.Flags().Changed("output") {
			outputDir, _ = cmd.Flags().GetString("output")
		} else if resolved.ArchiveDir != "" {
			outputDir = resolved.ArchiveDir
		}

		tracesDir := ""
		if resolved.TracesDir != "" {
			tracesDir = resolved.TracesDir
		}

		visualizerDir := ""
		if resolved.VisualizersDir != "" {
			visualizerDir = resolved.VisualizersDir
		}

		return webServeCommand(&webArgs{
			Port:          port,
			Open:          openFlag,
			DevMode:       devMode,
			ArchiveDir:    outputDir,
			TracesDir:     tracesDir,
			VisualizerDir: visualizerDir,
		})
	},
}

var webViewCmd = &cobra.Command{
	Use:   "view [ref]",
	Short: "Open a run or archive file in the browser",
	Long: `Open a specific run, batch, or archive file in the browser.

Accepts run IDs, batch IDs, or file paths:
  aat web view run-20260225-143022-abc12345
  aat web view path/to/exported.aar
  aat web view path/to/run-dir/archive.json
  aat web view path/to/batch.aab

When given a .aar, .aab, archive.json, or batch.json file, the archive is
loaded directly into memory — no project setup or manifest is needed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		port, _ := cmd.Flags().GetInt("port")

		// Check if the argument is a file path before resolving project context.
		if ref != "" {
			ft := classifyFileArg(ref)
			if ft != fileTypeNotAFile {
				return webViewFileCommand(port, ref, ft)
			}
		}

		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		outputDir := "_output/runs"
		if cmd.Flags().Changed("output") {
			outputDir, _ = cmd.Flags().GetString("output")
		} else if resolved.ArchiveDir != "" {
			outputDir = resolved.ArchiveDir
		}

		tracesDir := ""
		if resolved.TracesDir != "" {
			tracesDir = resolved.TracesDir
		}

		visualizerDir := ""
		if resolved.VisualizersDir != "" {
			visualizerDir = resolved.VisualizersDir
		}

		return webViewCommand(port, ref, outputDir, tracesDir, visualizerDir)
	},
}

var webViewTraceCmd = &cobra.Command{
	Use:   "viewtrace [id]",
	Short: "Open the trace viewer in the browser",
	Long:  "Open the trace list (or a specific trace) in the browser. If no server is running, starts a temporary one.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		port, _ := cmd.Flags().GetInt("port")

		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		outputDir := "_output/runs"
		if cmd.Flags().Changed("output") {
			outputDir, _ = cmd.Flags().GetString("output")
		} else if resolved.ArchiveDir != "" {
			outputDir = resolved.ArchiveDir
		}

		tracesDir := ""
		if resolved.TracesDir != "" {
			tracesDir = resolved.TracesDir
		}

		visualizerDir := ""
		if resolved.VisualizersDir != "" {
			visualizerDir = resolved.VisualizersDir
		}

		traceRef := ""
		if len(args) > 0 {
			traceRef = args[0]
		}

		return webViewTraceCommand(port, traceRef, outputDir, tracesDir, visualizerDir)
	},
}

func init() {
	webCmd.Flags().Int("port", 9119, "port to listen on")
	webCmd.Flags().Bool("open", false, "open browser after starting")
	webCmd.Flags().Bool("dev", false, "enable development mode (request logging)")
	webCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	webCmd.Flags().String("output", "_output/runs", "directory containing run archives")

	webViewCmd.Flags().Int("port", 9119, "port the server is running on")
	webViewCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	webViewCmd.Flags().String("output", "_output/runs", "directory containing run archives")

	webViewTraceCmd.Flags().Int("port", 9119, "port the server is running on")
	webViewTraceCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	webViewTraceCmd.Flags().String("output", "_output/runs", "directory containing run archives")

	webCmd.AddCommand(webViewCmd)
	webCmd.AddCommand(webViewTraceCmd)
}

// webArgs holds parsed CLI flags for the web serve command.
type webArgs struct {
	Port          int
	Open          bool
	DevMode       bool
	ArchiveDir    string
	TracesDir     string
	VisualizerDir string
	OpenURL       string // specific URL to open; if empty and Open is true, opens the base URL
}

// webServeCommand starts the web server and blocks until interrupted.
func webServeCommand(args *webArgs) error {
	srv := server.NewServer(server.ServerOptions{
		Port:          args.Port,
		ArchiveDir:    args.ArchiveDir,
		TracesDir:     args.TracesDir,
		VisualizerDir: args.VisualizerDir,
		DevMode:       args.DevMode,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	if args.Open {
		if waitForServer(srv, 2*time.Second) {
			url := args.OpenURL
			if url == "" {
				url = fmt.Sprintf("http://localhost:%d", args.Port)
			}
			if err := openURLFunc(url); err != nil {
				fmt.Fprintf(os.Stderr, "aat web: could not open browser: %s\n", err)
			}
		}
	}

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "aat web: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}

// webViewCommand opens a run in the browser. If no server is running on the
// given port, it starts a temporary one that serves until interrupted.
func webViewCommand(port int, ref string, archiveDir string, tracesDir string, visualizerDir string) error {
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	if err := checkServerHealth(baseURL); err == nil {
		// Server already running — just open the URL.
		url := buildViewURL(port, ref, archiveDir)
		return openURLFunc(url)
	}

	// No server running — start an ephemeral one and open the ref URL.
	fmt.Fprintf(os.Stderr, "aat web: no server running on port %d, starting one...\n", port)
	return webServeCommand(&webArgs{
		Port:          port,
		Open:          true,
		OpenURL:       buildViewURL(port, ref, archiveDir),
		ArchiveDir:    archiveDir,
		TracesDir:     tracesDir,
		VisualizerDir: visualizerDir,
	})
}

// webViewTraceCommand opens the trace viewer in the browser. If no server is
// running on the given port, it starts a temporary one that serves until interrupted.
func webViewTraceCommand(port int, traceRef string, archiveDir string, tracesDir string, visualizerDir string) error {
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	url := buildTraceViewURL(port, traceRef)

	if err := checkServerHealth(baseURL); err == nil {
		return openURLFunc(url)
	}

	fmt.Fprintf(os.Stderr, "aat web: no server running on port %d, starting one...\n", port)
	return webServeCommand(&webArgs{
		Port:          port,
		Open:          true,
		OpenURL:       url,
		ArchiveDir:    archiveDir,
		TracesDir:     tracesDir,
		VisualizerDir: visualizerDir,
	})
}

// buildViewURL constructs the URL for viewing a run or batch in the frontend.
// It checks directory contents to determine the type rather than relying on
// name prefixes, supporting named/renamed directories.
func buildViewURL(port int, ref string, archiveDir string) string {
	if ref == "" {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	if archiveDir != "" {
		batchPath := filepath.Join(archiveDir, ref, "batch.json")
		if _, err := os.Stat(batchPath); err == nil {
			return fmt.Sprintf("http://localhost:%d/batches/%s", port, ref)
		}
	}
	return fmt.Sprintf("http://localhost:%d/runs/%s", port, ref)
}

// buildTraceViewURL constructs the URL for viewing traces in the frontend.
func buildTraceViewURL(port int, traceRef string) string {
	if traceRef == "" {
		return fmt.Sprintf("http://localhost:%d/traces", port)
	}
	return fmt.Sprintf("http://localhost:%d/traces/%s", port, traceRef)
}

// checkServerHealth checks if the server is reachable via its health endpoint.
func checkServerHealth(baseURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return fmt.Errorf("server not reachable at %s: %w\n\nhint: start the server first with: aat web", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server at %s returned status %d\n\nhint: the server may not be ready yet", baseURL, resp.StatusCode)
	}
	return nil
}

// waitForServer polls until the server has a listening address or the timeout expires.
func waitForServer(srv *server.Server, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.Addr() != "" {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

// openURL opens a URL in the default browser.
func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

// --- File-based archive viewing ---

type fileType int

const (
	fileTypeNotAFile  fileType = iota
	fileTypeAar                // .aar zip (run)
	fileTypeAab                // .aab zip (batch)
	fileTypeRunJSON            // archive.json
	fileTypeBatchJSON          // batch.json
)

// classifyFileArg checks if the argument refers to a viewable archive file.
// Returns fileTypeNotAFile if the argument is not a file or not a recognized type.
func classifyFileArg(arg string) fileType {
	lower := strings.ToLower(arg)

	// Check extensions first (fast path).
	if strings.HasSuffix(lower, ".aar") {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return fileTypeAar
		}
	}
	if strings.HasSuffix(lower, ".aab") {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return fileTypeAab
		}
	}

	// Check for archive.json or batch.json by basename.
	base := filepath.Base(arg)
	if base == "archive.json" {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return fileTypeRunJSON
		}
	}
	if base == "batch.json" {
		if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
			return fileTypeBatchJSON
		}
	}

	return fileTypeNotAFile
}

// loadArchiveFile loads an archive file into a pre-loaded ArchiveService.
// Returns the service, the ref ID for URL building, and the archive type string.
func loadArchiveFile(filePath string, ft fileType) (server.ArchiveService, string, string, error) {
	switch ft {
	case fileTypeAar:
		main, attempts, err := archive.LoadRunFromZip(filePath)
		if err != nil {
			return nil, "", "", fmt.Errorf("loading .aar: %w", err)
		}
		id := main.Metadata.RunID
		if id == "" {
			id = "archive"
		}
		svc := server.NewArchiveServiceFromRun(id, main, attempts)
		return svc, id, "run", nil

	case fileTypeAab:
		batch, runs, attempts, err := archive.LoadBatchFromZip(filePath)
		if err != nil {
			return nil, "", "", fmt.Errorf("loading .aab: %w", err)
		}
		id := batch.Metadata.BatchID
		if id == "" {
			id = "batch"
		}
		svc := server.NewArchiveServiceFromBatch(id, batch, runs, attempts)
		return svc, id, "batch", nil

	case fileTypeRunJSON:
		main, err := archive.Read(filePath)
		if err != nil {
			return nil, "", "", fmt.Errorf("reading archive.json: %w", err)
		}
		// Load sibling attempt files from the same directory.
		attempts := loadSiblingAttempts(filepath.Dir(filePath))
		id := main.Metadata.RunID
		if id == "" {
			id = "archive"
		}
		svc := server.NewArchiveServiceFromRun(id, main, attempts)
		return svc, id, "run", nil

	case fileTypeBatchJSON:
		batch, err := archive.ReadBatch(filePath)
		if err != nil {
			return nil, "", "", fmt.Errorf("reading batch.json: %w", err)
		}
		// Load sub-run archives from the same directory.
		runs, attempts := loadBatchMemberRuns(filepath.Dir(filePath))
		id := batch.Metadata.BatchID
		if id == "" {
			id = "batch"
		}
		svc := server.NewArchiveServiceFromBatch(id, batch, runs, attempts)
		return svc, id, "batch", nil

	default:
		return nil, "", "", fmt.Errorf("unsupported file type")
	}
}

// loadSiblingAttempts scans dir for attempt-NN.json files and loads them.
func loadSiblingAttempts(dir string) map[int]*archive.Archive {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	attempts := make(map[int]*archive.Archive)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "attempt-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		a, err := archive.Read(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		// Parse attempt number from filename.
		numStr := strings.TrimPrefix(name, "attempt-")
		numStr = strings.TrimSuffix(numStr, ".json")
		var num int
		if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
			continue
		}
		attempts[num] = a
	}
	if len(attempts) == 0 {
		return nil
	}
	return attempts
}

// loadBatchMemberRuns scans dir for subdirectories containing archive.json
// and loads each as a member run.
func loadBatchMemberRuns(dir string) (map[string]*archive.Archive, map[string]map[int]*archive.Archive) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	runs := make(map[string]*archive.Archive)
	allAttempts := make(map[string]map[int]*archive.Archive)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		archivePath := filepath.Join(dir, e.Name(), "archive.json")
		a, err := archive.Read(archivePath)
		if err != nil {
			continue
		}
		runID := e.Name()
		runs[runID] = a
		// Load attempt files for this run.
		attempts := loadSiblingAttempts(filepath.Join(dir, e.Name()))
		if attempts != nil {
			allAttempts[runID] = attempts
		}
	}
	return runs, allAttempts
}

// webViewFileCommand loads an archive file into memory and serves it via
// an ephemeral web server. No temp files are created on disk.
func webViewFileCommand(port int, filePath string, ft fileType) error {
	svc, ref, archiveType, err := loadArchiveFile(filePath, ft)
	if err != nil {
		return err
	}

	var viewURL string
	if archiveType == "batch" {
		viewURL = fmt.Sprintf("http://localhost:%d/batches/%s", port, ref)
	} else {
		viewURL = fmt.Sprintf("http://localhost:%d/runs/%s", port, ref)
	}

	fmt.Fprintf(os.Stderr, "aat web: viewing %s (in-memory, no temp files)\n", filePath)

	srv := server.NewServerWithService(server.ServerOptions{
		Port: port,
	}, svc)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	if waitForServer(srv, 2*time.Second) {
		if err := openURLFunc(viewURL); err != nil {
			fmt.Fprintf(os.Stderr, "aat web: could not open browser: %s\n", err)
		}
	}

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "aat web: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}
