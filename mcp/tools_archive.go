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
	"github.com/gburgyan/aat/graph"
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

	s.mcp.AddTool(
		mcp.NewTool("list_recent_failures",
			mcp.WithDescription("List recent failed run archives, showing run ID, timestamp, outcome, and error summary. Skips passed runs."),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of failures to return (default 10)"),
			),
		),
		s.handleListRecentFailures,
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

// handleListRecentFailures scans archives and returns only failures.
func (s *Server) handleListRecentFailures(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	// Filter directories with "run-" prefix, sorted newest first.
	var runDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "run-") {
			runDirs = append(runDirs, e)
		}
	}

	sort.Slice(runDirs, func(i, j int) bool {
		return runDirs[i].Name() > runDirs[j].Name()
	})

	var b strings.Builder
	found := 0

	for _, dir := range runDirs {
		if found >= limit {
			break
		}
		archivePath := filepath.Join(s.ctx.ArchiveDir, dir.Name(), "archive.json")
		a, err := archive.Read(archivePath)
		if err != nil {
			continue
		}
		if a.Result.Outcome == "passed" {
			continue
		}
		found++
		fmt.Fprintf(&b, "- %s\n", formatArchiveListEntry(a))
	}

	if found == 0 {
		return mcp.NewToolResultText("No recent failures found."), nil
	}

	header := fmt.Sprintf("Found %d recent failure(s):\n\n", found)
	return mcp.NewToolResultText(header + b.String()), nil
}

// registerSampleResponseTool adds the get_sample_response tool.
func (s *Server) registerSampleResponseTool() {
	s.mcp.AddTool(
		mcp.NewTool("get_sample_response",
			mcp.WithDescription("Get a sample API response for an operation from run archives. Shows the response body, status code, and source archive. Useful for understanding response shapes and extract rules."),
			mcp.WithString("node",
				mcp.Description("Operation/node name to get a sample response for"),
				mcp.Required(),
			),
			mcp.WithString("run_id",
				mcp.Description("Specific run ID to get the response from (optional — defaults to searching recent archives)"),
			),
		),
		s.handleGetSampleResponse,
	)
}

// handleGetSampleResponse returns a sample response body for the given node
// from run archives.
func (s *Server) handleGetSampleResponse(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: node"), nil
	}

	if s.ctx.ArchiveDir == "" {
		return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml"), nil
	}

	// Verify node exists in graph.
	if s.ctx.Graph.Nodes[nodeName] == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q — if this is a workflow step ID, check the workflow detail for the underlying operation name", nodeName)), nil
	}

	runID, _ := req.RequireString("run_id")

	step, sourceRunID, err := findSampleResponse(s.ctx.ArchiveDir, nodeName, runID)
	if err != nil {
		// If explicit run_id was given, always return error.
		// For auto-discover (no run_id), return helpful fallback.
		if runID != "" {
			return mcp.NewToolResultError(err.Error()), nil
		}
		node := s.ctx.Graph.Nodes[nodeName]
		return mcp.NewToolResultText(s.formatNoArchiveFallback(nodeName, node)), nil
	}

	return mcp.NewToolResultText(formatSampleResponse(step, nodeName, sourceRunID)), nil
}

// findSampleResponse searches archives for a successful step matching nodeName.
// If runID is provided, searches only that archive. Otherwise scans recent archives
// newest first.
func findSampleResponse(archiveDir, nodeName, runID string) (*archive.StepRecord, string, error) {
	if runID != "" {
		a, err := loadArchive(archiveDir, runID)
		if err != nil {
			return nil, "", err
		}
		for i := range a.Steps {
			if a.Steps[i].Node == nodeName && a.Steps[i].Response != nil {
				return &a.Steps[i], runID, nil
			}
		}
		return nil, "", fmt.Errorf("node %q not found in archive %q", nodeName, runID)
	}

	// Scan recent archives (newest first).
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no archives found — run the integration first")
		}
		return nil, "", fmt.Errorf("reading archive directory: %v", err)
	}

	// Filter and sort run directories newest first.
	var runDirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "run-") {
			runDirs = append(runDirs, e)
		}
	}
	sort.Slice(runDirs, func(i, j int) bool {
		return runDirs[i].Name() > runDirs[j].Name()
	})

	for _, dir := range runDirs {
		archivePath := filepath.Join(archiveDir, dir.Name(), "archive.json")
		a, err := archive.Read(archivePath)
		if err != nil {
			continue
		}
		for i := range a.Steps {
			if a.Steps[i].Node == nodeName && a.Steps[i].Response != nil {
				return &a.Steps[i], dir.Name(), nil
			}
		}
	}

	return nil, "", fmt.Errorf("no sample response found for node %q in any archive — run the integration first", nodeName)
}

// formatSampleResponse renders a sample response as Markdown.
func formatSampleResponse(step *archive.StepRecord, nodeName, runID string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Sample Response: %s\n\n", nodeName)
	fmt.Fprintf(&b, "**Source:** %s\n", runID)
	fmt.Fprintf(&b, "**Status:** %d\n", step.Response.Status)
	fmt.Fprintf(&b, "**Duration:** %s\n", formatDurationMs(step.DurationMs))

	if len(step.Response.Body) > 0 {
		b.WriteString("\n## Response Body\n\n```json\n")
		b.WriteString(truncateBody(step.Response.Body, 4000))
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	if len(step.Outputs) > 0 {
		b.WriteString("\n## Extracted Outputs\n\n")
		b.WriteString("| Name | Value |\n")
		b.WriteString("|------|-------|\n")
		keys := make([]string, 0, len(step.Outputs))
		for k := range step.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := fmt.Sprintf("%v", step.Outputs[k])
			if len(val) > 80 {
				val = val[:77] + "..."
			}
			fmt.Fprintf(&b, "| %s | %s |\n", k, val)
		}
	}

	return b.String()
}

// formatNoArchiveFallback returns helpful information when no sample response
// is available. Shows graph output shape, extract rules, and suggests
// the inspect_request_template tool.
func (s *Server) formatNoArchiveFallback(nodeName string, node *graph.Node) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# No Sample Response Yet: %s\n\n", nodeName)
	b.WriteString("No run archives contain a response for this operation. ")
	b.WriteString("Run the integration first to capture sample responses.\n\n")

	// Suggest inspect_request_template
	adapterName := nodeName
	if node.Adapter != "" {
		adapterName = node.Adapter
	}
	fmt.Fprintf(&b, "**Tip:** Use `inspect_request_template` with adapter `%s` to see the request shape and extract rules.\n\n", adapterName)

	// Show graph output shape
	if len(node.Outputs) > 0 {
		b.WriteString("## Expected Output Shape\n\n")
		b.WriteString(formatOutputTable(node.Outputs))
	}

	// Show extract rules from template if available
	if tmpl, ok := s.ctx.Registry.GetTemplate(adapterName); ok && len(tmpl.Response.Extract) > 0 {
		b.WriteString("\n## Extract Rule Paths\n\n")

		if envelope := detectResponseEnvelope(tmpl.Response.Extract); envelope != "" {
			fmt.Fprintf(&b, "> **Response envelope:** The JSON response root key is `%s`.\n\n", envelope)
		}

		b.WriteString("| Output | Path |\n")
		b.WriteString("|--------|------|\n")
		keys := make([]string, 0, len(tmpl.Response.Extract))
		for k := range tmpl.Response.Extract {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rule := tmpl.Response.Extract[k]
			fmt.Fprintf(&b, "| %s | %s |\n", k, rule.Path)
		}
	}

	return b.String()
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
