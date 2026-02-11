package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gburgyan/aat/archive"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerArchiveTools adds archive inspection tools to the MCP server.
func (s *Server) registerArchiveTools() {
	s.mcp.AddTool(
		mcp.NewTool("list_archives",
			mcp.WithDescription("List recent run archives from the archive directory, showing run ID, timestamp, outcome, and duration."),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of archives to return (default 10)"),
			),
		),
		s.handleListArchives,
	)

	s.mcp.AddTool(
		mcp.NewTool("inspect_archive",
			mcp.WithDescription("Show detailed Markdown view of a run archive including per-step request/response data, assertions, selections, and value resolutions."),
			mcp.WithString("run_id",
				mcp.Description("Run ID (e.g. 'run-20260210-143000-abcd1234')"),
				mcp.Required(),
			),
		),
		s.handleInspectArchive,
	)

	s.mcp.AddTool(
		mcp.NewTool("analyze_failure",
			mcp.WithDescription("Analyze a failed run archive and provide failure-focused diagnostics with suggested next steps."),
			mcp.WithString("run_id",
				mcp.Description("Run ID of the failed run to analyze"),
				mcp.Required(),
			),
		),
		s.handleAnalyzeFailure,
	)

	s.mcp.AddTool(
		mcp.NewTool("diff_archives",
			mcp.WithDescription("Side-by-side comparison of two run archives showing outcome, status, duration, and output differences."),
			mcp.WithString("run_id_1",
				mcp.Description("First run ID"),
				mcp.Required(),
			),
			mcp.WithString("run_id_2",
				mcp.Description("Second run ID"),
				mcp.Required(),
			),
		),
		s.handleDiffArchives,
	)
}

// handleListArchives scans the archive directory for recent runs.
func (s *Server) handleListArchives(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultText(
			"Archive directory not configured. Set the `archives` field in aat-project.yaml to enable archive inspection.",
		), nil
	}

	limit := 10
	if v, err := req.RequireFloat("limit"); err == nil {
		limit = int(v)
		if limit < 1 {
			limit = 1
		}
	}

	entries, err := os.ReadDir(s.ctx.ArchiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultText("Archive directory is empty (no runs yet)."), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("reading archive directory: %v", err)), nil
	}

	// Filter directories with "run-" prefix
	var runDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "run-") {
			runDirs = append(runDirs, e)
		}
	}

	if len(runDirs) == 0 {
		return mcp.NewToolResultText("No archives found."), nil
	}

	// Sort descending by name (timestamp-based, so newest first)
	sort.Slice(runDirs, func(i, j int) bool {
		return runDirs[i].Name() > runDirs[j].Name()
	})

	// Limit
	if len(runDirs) > limit {
		runDirs = runDirs[:limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d archive(s):\n\n", len(runDirs))

	for _, dir := range runDirs {
		archivePath := filepath.Join(s.ctx.ArchiveDir, dir.Name(), "archive.json")
		a, err := archive.Read(archivePath)
		if err != nil {
			fmt.Fprintf(&b, "- **%s** — (parse error)\n", dir.Name())
			continue
		}
		fmt.Fprintf(&b, "- %s\n", formatArchiveListEntry(a))
	}

	return mcp.NewToolResultText(b.String()), nil
}

// handleInspectArchive loads and displays a detailed archive view.
func (s *Server) handleInspectArchive(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runID, err := req.RequireString("run_id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: run_id"), nil
	}

	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml"), nil
	}

	a, err := loadArchive(s.ctx.ArchiveDir, runID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(formatArchiveDetail(a)), nil
}

// handleAnalyzeFailure provides failure-focused diagnostics.
func (s *Server) handleAnalyzeFailure(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runID, err := req.RequireString("run_id")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: run_id"), nil
	}

	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml"), nil
	}

	a, err := loadArchive(s.ctx.ArchiveDir, runID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(formatFailureAnalysis(a)), nil
}

// handleDiffArchives compares two run archives side-by-side.
func (s *Server) handleDiffArchives(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runID1, err := req.RequireString("run_id_1")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: run_id_1"), nil
	}
	runID2, err := req.RequireString("run_id_2")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: run_id_2"), nil
	}

	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml"), nil
	}

	if runID1 == runID2 {
		return mcp.NewToolResultError("cannot diff an archive with itself — provide two different run IDs"), nil
	}

	a1, err := loadArchive(s.ctx.ArchiveDir, runID1)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("run 1: %s", err)), nil
	}

	a2, err := loadArchive(s.ctx.ArchiveDir, runID2)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("run 2: %s", err)), nil
	}

	return mcp.NewToolResultText(formatArchiveDiff(a1, a2)), nil
}

// loadArchive loads an archive from the archive directory by run ID.
func loadArchive(archiveDir, runID string) (*archive.Archive, error) {
	archivePath := filepath.Join(archiveDir, runID, "archive.json")
	a, err := archive.Read(archivePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("archive %q not found", runID)
		}
		return nil, fmt.Errorf("reading archive: %v", err)
	}
	return a, nil
}
