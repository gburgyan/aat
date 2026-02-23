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
	"syscall"
	"time"

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

		return webServeCommand(&webArgs{
			Port:       port,
			Open:       openFlag,
			DevMode:    devMode,
			ArchiveDir: outputDir,
			TracesDir:  tracesDir,
		})
	},
}

var webViewCmd = &cobra.Command{
	Use:   "view [ref]",
	Short: "Open a run in the browser",
	Long:  "Open a specific run (or the latest) in the browser. If no server is running, starts a temporary one.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		ref := "latest"
		if len(args) > 0 {
			ref = args[0]
		}
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

		return webViewCommand(port, ref, outputDir, tracesDir)
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

		traceRef := ""
		if len(args) > 0 {
			traceRef = args[0]
		}

		return webViewTraceCommand(port, traceRef, outputDir, tracesDir)
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
	Port       int
	Open       bool
	DevMode    bool
	ArchiveDir string
	TracesDir  string
	OpenURL    string // specific URL to open; if empty and Open is true, opens the base URL
}

// webServeCommand starts the web server and blocks until interrupted.
func webServeCommand(args *webArgs) error {
	srv := server.NewServer(server.ServerOptions{
		Port:       args.Port,
		ArchiveDir: args.ArchiveDir,
		TracesDir:  args.TracesDir,
		DevMode:    args.DevMode,
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
func webViewCommand(port int, ref string, archiveDir string, tracesDir string) error {
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	if err := checkServerHealth(baseURL); err == nil {
		// Server already running — just open the URL.
		url := buildViewURL(port, ref, archiveDir)
		return openURLFunc(url)
	}

	// No server running — start an ephemeral one and open the ref URL.
	fmt.Fprintf(os.Stderr, "aat web: no server running on port %d, starting one...\n", port)
	return webServeCommand(&webArgs{
		Port:       port,
		Open:       true,
		OpenURL:    buildViewURL(port, ref, archiveDir),
		ArchiveDir: archiveDir,
		TracesDir:  tracesDir,
	})
}

// webViewTraceCommand opens the trace viewer in the browser. If no server is
// running on the given port, it starts a temporary one that serves until interrupted.
func webViewTraceCommand(port int, traceRef string, archiveDir string, tracesDir string) error {
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	url := buildTraceViewURL(port, traceRef)

	if err := checkServerHealth(baseURL); err == nil {
		return openURLFunc(url)
	}

	fmt.Fprintf(os.Stderr, "aat web: no server running on port %d, starting one...\n", port)
	return webServeCommand(&webArgs{
		Port:       port,
		Open:       true,
		OpenURL:    url,
		ArchiveDir: archiveDir,
		TracesDir:  tracesDir,
	})
}

// buildViewURL constructs the URL for viewing a run or batch in the frontend.
// It checks directory contents to determine the type rather than relying on
// name prefixes, supporting named/renamed directories.
func buildViewURL(port int, ref string, archiveDir string) string {
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
	defer resp.Body.Close()

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
