package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gburgyan/aat/archive"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/validate"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tidwall/gjson"
)

// maxSampleShapeBytes caps the shape get_sample_response returns.
const maxSampleShapeBytes = 8000

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
			mcp.WithDescription("Get a sample API response for an operation from run archives: the newest successful response, or the newest failed one when no run succeeded. Shows the response body, status code, and source run. `path` narrows the body with a gjson path, and `shape` returns the body's structure instead of its values (each path with its type, array sizes, and a sample value), which suits large responses. Useful for understanding response shapes and extract rules."),
			mcp.WithString("node",
				mcp.Description("Operation/node name to get a sample response for"),
				mcp.Required(),
			),
			mcp.WithString("run_id",
				mcp.Description("Run to get the response from: a run ID, a batch ID joined by a slash to a run ID or to the name of a plan the batch ran, or latest (optional — defaults to searching recent archives)"),
			),
			mcp.WithString("path",
				mcp.Description("gjson path selecting part of the response body, such as data.items.0 or data.items.#.id (optional)"),
			),
			mcp.WithBoolean("shape",
				mcp.Description("Return the structure of the body (or of what path selects) instead of its values (optional)"),
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

	// Verify node exists in graph.
	if s.ctx.Graph.Nodes[nodeName] == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown node %q — if this is a workflow step ID, check the workflow detail for the underlying operation name", nodeName)), nil
	}

	runID, _ := req.RequireString("run_id")
	node := s.ctx.Graph.Nodes[nodeName]

	// Without an archive directory a specific run cannot be found, but the
	// expected output shape still helps.
	if s.ctx.ArchiveDir == "" {
		if runID != "" {
			return mcp.NewToolResultError("archive directory not configured — set the `archives` field in aat-project.yaml"), nil
		}
		return mcp.NewToolResultText(s.formatNoArchiveFallback(nodeName, node)), nil
	}

	step, sourceRunID, err := findSampleResponse(s.ctx.ArchiveDir, nodeName, runID)
	if err != nil {
		// If explicit run_id was given, always return error.
		// For auto-discover (no run_id), return helpful fallback.
		if runID != "" {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(s.formatNoArchiveFallback(nodeName, node)), nil
	}

	view := sampleView{path: req.GetString("path", ""), shape: req.GetBool("shape", false)}
	return mcp.NewToolResultText(formatSampleResponse(step, nodeName, sourceRunID, view)), nil
}

// findSampleResponse finds an archived response for nodeName, preferring a
// successful (2xx) one. With runID it searches that run, which may belong to a
// batch. Otherwise it scans every run, runs inside batches included, newest
// first, and returns the newest successful response, or the newest response of
// any status when none succeeded.
func findSampleResponse(archiveDir, nodeName, runID string) (*archive.StepRecord, string, error) {
	if runID != "" {
		run, a, err := loadRun(archiveDir, runID)
		if err != nil {
			return nil, "", err
		}
		if step := sampleStep(a, nodeName); step != nil {
			return step, run.Ref(), nil
		}
		return nil, "", fmt.Errorf("node %q not found in archive %q", nodeName, runID)
	}

	runs, err := archive.ListRuns(archiveDir)
	if err != nil {
		return nil, "", err
	}

	var fallback *archive.StepRecord
	var fallbackRunID string
	for _, run := range runs {
		a, err := archive.Read(run.ArchivePath)
		if err != nil {
			continue
		}
		step := sampleStep(a, nodeName)
		switch {
		case step == nil:
		case isSuccessStatus(step.Response.Status):
			return step, run.Ref(), nil
		case fallback == nil:
			fallback, fallbackRunID = step, run.Ref()
		}
	}
	if fallback != nil {
		return fallback, fallbackRunID, nil
	}
	return nil, "", fmt.Errorf("no sample response found for node %q in any archive — run the integration first", nodeName)
}

// sampleStep returns the step of a that best shows nodeName's response: its
// first successful one, else its first with any response, else nil.
func sampleStep(a *archive.Archive, nodeName string) *archive.StepRecord {
	var first *archive.StepRecord
	for i := range a.Steps {
		step := &a.Steps[i]
		if step.Node != nodeName || step.Response == nil {
			continue
		}
		if isSuccessStatus(step.Response.Status) {
			return step
		}
		if first == nil {
			first = step
		}
	}
	return first
}

// isSuccessStatus reports whether an HTTP status is 2xx.
func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

// sampleView holds how get_sample_response shows a body: narrowed to a gjson
// path, and as its shape instead of its values.
type sampleView struct {
	path  string
	shape bool
}

// formatSampleResponse renders a sample response as Markdown.
func formatSampleResponse(step *archive.StepRecord, nodeName, runID string, view sampleView) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Sample Response: %s\n\n", nodeName)
	fmt.Fprintf(&b, "**Source:** %s\n", runID)
	fmt.Fprintf(&b, "**Status:** %d\n", step.Response.Status)
	fmt.Fprintf(&b, "**Duration:** %s\n", formatDurationMs(step.DurationMs))
	if !isSuccessStatus(step.Response.Status) {
		b.WriteString("\n**Note:** no successful response was found; this one failed. Run a plan that calls this operation successfully for a representative sample.\n")
	}

	if body := step.Response.Body; len(body) > 0 {
		if view.path != "" {
			result := gjson.GetBytes(body, validate.NormalizeJSONPath(view.path))
			if result.Exists() {
				fmt.Fprintf(&b, "**Path:** `%s`\n", view.path)
				body = []byte(result.Raw)
			} else {
				fmt.Fprintf(&b, "\n**Path:** `%s` matches nothing in the response body.\n", view.path)
				body = nil
			}
		}
		switch {
		case body == nil:
		case view.shape:
			b.WriteString("\n## Response Shape\n\nEach gjson path with its type, array size, how many objects hold the key when not all do, and a sample value.\n\n```\n")
			b.WriteString(cutShape(archive.RenderShape(archive.Shape(body)), maxSampleShapeBytes))
			b.WriteString("```\n")
		default:
			b.WriteString("\n## Response Body\n\n```json\n")
			b.WriteString(truncateBody(body, 4000))
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n")
		}
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

// cutShape cuts rendered shape text after maxBytes, at a line boundary.
func cutShape(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	cut := text[:maxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		cut = cut[:i+1]
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "... (truncated)\n"
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

// loadArchive loads a run's archive from the archive directory: by run ID, by
// batch ID joined by a slash to a run ID or a plan name, or latest.
func loadArchive(archiveDir, runID string) (*archive.Archive, error) {
	_, a, err := loadRun(archiveDir, runID)
	return a, err
}

// loadRun resolves runID with archive.FindRun, which never leaves the archive
// directory, and reads the run's archive. A plan name with several runs in its
// batch returns FindRun's error, which lists them.
func loadRun(archiveDir, runID string) (archive.RunRef, *archive.Archive, error) {
	run, err := archive.FindRun(archiveDir, runID)
	if errors.Is(err, archive.ErrRunNotFound) {
		return run, nil, fmt.Errorf("archive %q not found", runID)
	}
	if errors.Is(err, archive.ErrAmbiguousRun) {
		return run, nil, err
	}
	if err != nil {
		return run, nil, fmt.Errorf("reading archive: %v", err)
	}
	a, err := archive.Read(run.ArchivePath)
	if err != nil {
		return run, nil, fmt.Errorf("reading archive: %v", err)
	}
	return run, a, nil
}
