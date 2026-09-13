package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aat "github.com/gburgyan/aat"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/internal/sandbox/shop"
	aatmcp "github.com/gburgyan/aat/mcp"
)

// TestShopExample runs the embedded examples/shop project against the
// in-process sandbox: the checks the CI example-shop job runs with the real
// binaries (strict validation, every plan in both regions with strict OpenAPI
// validation, the layer matrix and its dedup counts, the declined-card overlay,
// a checkpoint handoff, the packaged integration kit) plus the retry demo. Each
// subtest gets its own sandbox and project copy, so IDs and chaos counters are
// deterministic.
func TestShopExample(t *testing.T) {
	if testing.Short() {
		t.Skip("shop example end-to-end test skipped in -short mode")
	}

	t.Run("validate strict", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		var out bytes.Buffer
		code := validateCommand(&validateArgs{ManifestPath: p.manifest, Strict: true, Vars: p.vars()}, &out)
		assert.Equal(t, 0, code, out.String())
	})

	for _, env := range []string{"us", "eu"} {
		t.Run("batch "+env, func(t *testing.T) {
			t.Parallel()
			p := newShopProject(t)

			res := batchCommand(context.Background(), &batchArgs{
				runArgs:  p.runArgs(t, env),
				PlanDirs: []string(p.m.PlanDirs),
			}, io.Discard)
			require.NoError(t, res.err)
			require.NotNil(t, res.summary)
			assert.Equal(t, "passed", res.summary.Outcome, failedRuns(res.summary))
			assert.Equal(t, 7, res.summary.Summary.TotalPlans)
			assert.Equal(t, 7, res.summary.Summary.PassedPlans)
		})
	}

	t.Run("layer matrix", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		args := &batchArgs{runArgs: p.runArgs(t, "us"), PlanDirs: []string(p.m.PlanDirs), Parallel: 4}
		args.LayerGroups = [][]string{
			{"shipping-standard", "shipping-express"},
			{"basket-gear", "basket-apparel"},
		}
		res := batchCommand(context.Background(), args, io.Discard)
		require.NoError(t, res.err)
		require.NotNil(t, res.summary)
		stats := res.summary.Summary
		assert.Equal(t, "passed", res.summary.Outcome, failedRuns(res.summary))
		assert.Equal(t, 63, stats.TotalPlans, "7 plans x 9 permutations")
		assert.Equal(t, 36, stats.SkippedPlans, "duplicate permutations are skipped")
		assert.Equal(t, 27, stats.PassedPlans, failedRuns(res.summary))
	})

	t.Run("declined-card overlay", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "smoke")
		args.EnvOverlay = filepath.Join(p.dir, "overlays", "declined-card.yaml")
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		assert.Equal(t, engine.OutcomePassed, res.outcome)
		assert.Equal(t, 402, stepByNode(t, res.summary, "paymentCharge").Status)
	})

	t.Run("escaped request values", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		// Quotes, a backslash, a newline, and URL-special characters reach the
		// API unchanged, and the body stays valid JSON under strict OAS checks.
		const notes = "Leave at \"door\" #2 \\ back\nring twice & wait?"
		overlay := filepath.Join(p.dir, "notes.yaml")
		require.NoError(t, os.WriteFile(overlay, []byte(fmt.Sprintf("overrides:\n  - match: checkoutCart\n    values:\n      notes: %q\n", notes)), 0o644))

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "smoke")
		args.EnvOverlay = overlay
		args.OASValidateMode = "strict"
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		require.Equal(t, engine.OutcomePassed, res.outcome)

		data, err := os.ReadFile(res.archivePath)
		require.NoError(t, err)
		var archived struct {
			Steps []struct {
				Node    string `json:"node"`
				Request struct {
					Body json.RawMessage `json:"body"`
				} `json:"request"`
			} `json:"steps"`
		}
		require.NoError(t, json.Unmarshal(data, &archived))
		for _, step := range archived.Steps {
			if step.Node == "checkoutCart" {
				var body map[string]any
				require.NoError(t, json.Unmarshal(step.Request.Body, &body), "the request body is valid JSON")
				assert.Equal(t, notes, body["notes"])
				return
			}
		}
		t.Fatal("the archive has no checkoutCart step")
	})

	t.Run("archive redaction", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "full-lifecycle")
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		require.NotEmpty(t, res.archivePath)
		data, err := os.ReadFile(res.archivePath)
		require.NoError(t, err)

		archived := string(data)
		assert.NotContains(t, archived, "pay-demo-key", "the payments API key is a secret")
		assert.NotContains(t, archived, "aat-shop-secret", "the OAuth2 client secret is a secret")
		assert.Contains(t, archived, "demo@example.com", "the short demo password does not mangle the email")
		assert.Contains(t, archived, `"Authorization": "[REDACTED]"`)
	})

	t.Run("resilience retries", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "resilience")
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		assert.Equal(t, engine.OutcomePassed, res.outcome)
		assert.Equal(t, 1, stepByNode(t, res.summary, "checkInventory").Retries, "one response_error retry")
		assert.Equal(t, 2, stepByNode(t, res.summary, "getShipment").Retries, "two transient retries")
		// Retry waits count toward the steps and the run: checkInventory's backoff
		// of at least 375ms (500ms less 25% jitter), and getShipment's two waits
		// of a full second each, because the sandbox's 503s send Retry-After: 1.
		assert.GreaterOrEqual(t, stepByNode(t, res.summary, "getShipment").DurationMs, int64(2000), "the step's duration includes its retry waits")
		assert.GreaterOrEqual(t, res.summary.Summary.DurationMs, int64(2375), "the run's duration is wall-clock time")
	})

	t.Run("checkpoint dump redacts credentials", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "smoke")
		args.StopAfterStep = "paymentCharge"
		args.DumpStatePath = filepath.Join(t.TempDir(), "state.json")
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		require.Equal(t, engine.OutcomeStopped, res.outcome)

		data, err := os.ReadFile(args.DumpStatePath)
		require.NoError(t, err)
		var state engine.StateExport
		require.NoError(t, json.Unmarshal(data, &state))
		assert.True(t, state.Redacted)
		assert.Equal(t, "[REDACTED]", state.Auth.Headers["Authorization"], "the shop's OAuth2 token")
		assert.NotEmpty(t, state.Values["checkout.orderId"], "IDs stay, so a harness can find what the run created")
		var payment *engine.StateStep
		for i := range state.Steps {
			if state.Steps[i].Node == "paymentCharge" {
				payment = &state.Steps[i]
			}
		}
		require.NotNil(t, payment, "the payment step is in the export")
		assert.Equal(t, "[REDACTED]", payment.Headers["X-API-Key"], "the payments API key")

		dump := string(data)
		assert.NotContains(t, dump, "Bearer ")
		assert.NotContains(t, dump, "pay-demo-key")
		assert.NotContains(t, dump, "aat-shop-secret")
	})

	t.Run("issued token redacted wherever it appears", func(t *testing.T) {
		t.Parallel()
		// The shop API echoes the bearer token in a response header that no
		// redaction rule names, so only knowing the issued token as a secret
		// keeps it out of the archive and the dump.
		seen := make(chan string, 100)
		p := newShopProjectWith(t, func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
					select {
					case seen <- token:
					default:
					}
					w.Header().Set("X-Echo-Token", token)
				}
				h.ServeHTTP(w, r)
			})
		})

		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "smoke")
		args.DumpStatePath = filepath.Join(t.TempDir(), "state.json")
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		require.Equal(t, engine.OutcomePassed, res.outcome)
		require.NotEmpty(t, res.archivePath)

		archived, err := os.ReadFile(res.archivePath)
		require.NoError(t, err)
		dump, err := os.ReadFile(args.DumpStatePath)
		require.NoError(t, err)
		require.NotZero(t, len(seen), "the shop API saw a bearer token")
		for len(seen) > 0 {
			token := <-seen
			assert.NotContains(t, string(archived), token)
			assert.NotContains(t, string(dump), token)
		}
		assert.Contains(t, string(archived), `"X-Echo-Token": "[REDACTED]"`)
	})

	t.Run("checkpoint handoff", func(t *testing.T) {
		t.Parallel()
		p := newShopProject(t)

		// Stop after the payment, which runs on the payments host with its own
		// API key: the export must still carry the shop session at top level.
		// The replay below sends the dumped token, so it asks for live credentials.
		args := p.runArgs(t, "us")
		args.PlanPath = p.plan(t, "smoke")
		args.StopAfterStep = "paymentCharge"
		args.DumpStatePath = filepath.Join(t.TempDir(), "state.json")
		args.DumpStateSecrets = true
		res := runCommand(context.Background(), &args, io.Discard, TerminalInfo{})
		require.NoError(t, res.err)
		assert.Equal(t, engine.OutcomeStopped, res.outcome)

		data, err := os.ReadFile(args.DumpStatePath)
		require.NoError(t, err)
		var state engine.StateExport
		require.NoError(t, json.Unmarshal(data, &state))
		orderID, ok := state.Values["checkout.orderId"].(string)
		require.True(t, ok, "state values: %v", state.Values)
		assert.Equal(t, p.apiURL+"/us/v1", state.BaseURL)
		assert.True(t, strings.HasPrefix(state.Auth.Headers["Authorization"], "Bearer "), "top-level auth is the shop bearer: %v", state.Auth.Headers)

		var payment *engine.StateStep
		for i := range state.Steps {
			if state.Steps[i].Node == "paymentCharge" {
				payment = &state.Steps[i]
			}
		}
		require.NotNil(t, payment, "the payment step is in the export")
		assert.Equal(t, p.payURL+"/us/v1", payment.BaseURL)
		assert.Equal(t, "pay-demo-key", payment.Headers["X-API-Key"])
		assert.Empty(t, payment.Headers["Authorization"], "the bearer token never went to the payments host")

		// Cleanup was skipped, so the dumped bearer token reads the live, paid order.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, p.apiURL+"/us/v1/orders/"+orderID, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", state.Auth.Headers["Authorization"])
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var order struct {
			Status string `json:"status"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&order))
		assert.Equal(t, "paid", order.Status)
	})

	t.Run("published kit", func(t *testing.T) {
		t.Parallel()
		shell, err := exec.LookPath("sh")
		if err != nil {
			t.Skip("packaging the kit needs sh")
		}
		if _, err := exec.LookPath("tar"); err != nil {
			t.Skip("packaging the kit needs tar")
		}
		p := newShopProject(t)

		// Package the kit as CI does and unpack the tarball into an empty
		// directory, so the checks see only what an integrator receives.
		out := filepath.Join(t.TempDir(), "shop-kit")
		packaged, err := exec.Command(shell, filepath.Join(p.dir, "package-kit.sh"), out).CombinedOutput()
		require.NoError(t, err, string(packaged))
		unpacked := t.TempDir()
		untarred, err := exec.Command("tar", "-xzf", out+".tar.gz", "-C", unpacked).CombinedOutput()
		require.NoError(t, err, string(untarred))

		kit := &shopProject{dir: filepath.Join(unpacked, "shop-kit"), apiURL: p.apiURL, payURL: p.payURL}
		kit.manifest = filepath.Join(kit.dir, "aat-project.yaml")
		kit.m, err = config.LoadManifest(kit.manifest)
		require.NoError(t, err)
		assert.NoDirExists(t, filepath.Join(kit.dir, "internal"), "internal suites stay out of the kit")
		assert.NoDirExists(t, filepath.Join(kit.dir, "layers"), "layers stay out of the kit")

		var validateOut bytes.Buffer
		code := validateCommand(&validateArgs{ManifestPath: kit.manifest, Strict: true, Vars: kit.vars()}, &validateOut)
		assert.Equal(t, 0, code, validateOut.String())

		res := batchCommand(context.Background(), &batchArgs{
			runArgs:  kit.runArgs(t, "us"),
			PlanDirs: []string(kit.m.PlanDirs),
		}, io.Discard)
		require.NoError(t, res.err)
		require.NotNil(t, res.summary)
		assert.Equal(t, "passed", res.summary.Outcome, failedRuns(res.summary))
		assert.Equal(t, 3, res.summary.Summary.TotalPlans, "the reference plans")

		// An integrator's AI tool loads the whole API from the kit alone.
		mcpCtx, err := aatmcp.BuildServerContextWithVars(kit.m, kit.vars())
		require.NoError(t, err)
		assert.Len(t, mcpCtx.Graph.Nodes, 17)
		assert.NotEmpty(t, mcpCtx.OASSpecs, "the graph's OpenAPI spec is in the kit")
	})
}

// shopProject is a copy of the embedded examples/shop project and the
// in-process sandbox its runs target.
type shopProject struct {
	dir      string
	manifest string
	apiURL   string
	payURL   string
	m        *config.ProjectManifest
}

// newShopProject starts a sandbox on random ports and extracts the embedded
// example into a temporary directory. Runs reach the sandbox through --var
// overrides of the env.yaml apiHost and payHost vars (see vars).
func newShopProject(t *testing.T) *shopProject {
	t.Helper()
	return newShopProjectWith(t, func(h http.Handler) http.Handler { return h })
}

// newShopProjectWith is newShopProject with wrap around both sandbox handlers,
// so a test can observe the requests that reach them.
func newShopProjectWith(t *testing.T, wrap func(http.Handler) http.Handler) *shopProject {
	t.Helper()

	srv := shop.New(shop.Options{Latency: 0, Seed: 1})
	api := httptest.NewServer(wrap(srv.APIHandler()))
	t.Cleanup(api.Close)
	pay := httptest.NewServer(wrap(srv.PaymentsHandler()))
	t.Cleanup(pay.Close)

	fsys, err := aat.ShopExampleFS()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, fsys))

	manifest := filepath.Join(dir, "aat-project.yaml")
	m, err := config.LoadManifest(manifest)
	require.NoError(t, err)
	return &shopProject{dir: dir, manifest: manifest, apiURL: api.URL, payURL: pay.URL, m: m}
}

// vars points env.yaml's apiHost and payHost vars at this project's sandbox,
// as `--var apiHost=… --var payHost=…` would.
func (p *shopProject) vars() map[string]string {
	return map[string]string{
		"apiHost": strings.TrimPrefix(p.apiURL, "http://"),
		"payHost": strings.TrimPrefix(p.payURL, "http://"),
	}
}

// runArgs returns run arguments resolved from the project manifest, as `aat run`
// would, for the named environment with strict OpenAPI validation.
func (p *shopProject) runArgs(t *testing.T, env string) runArgs {
	return runArgs{
		EnvPath:         p.m.EnvPath,
		EnvName:         env,
		GraphPath:       p.m.GraphPath,
		TemplatesPath:   p.m.TemplatesPath,
		DomainPath:      p.m.DomainPath,
		LayersDir:       p.m.LayersDir,
		OutputDir:       filepath.Join(t.TempDir(), "runs"),
		Quiet:           true,
		NoAutoOverrides: true,
		OASValidateMode: "strict",
		Vars:            p.vars(),
	}
}

// plan returns the path of the named plan, looked up in the manifest's plan
// directories as `aat run plan <name>` does.
func (p *shopProject) plan(t *testing.T, name string) string {
	t.Helper()
	path, err := config.FindPlan(p.m.PlanDirs, name)
	require.NoError(t, err)
	return path
}

// stepByNode returns the summary of the first step that ran node.
func stepByNode(t *testing.T, summary *RunSummary, node string) StepSummary {
	t.Helper()
	require.NotNil(t, summary)
	for _, s := range summary.Steps {
		if s.Node == node {
			return s
		}
	}
	require.Failf(t, "step not found", "no step ran node %q", node)
	return StepSummary{}
}

// failedRuns lists the batch runs that neither passed nor were skipped, for
// assertion messages.
func failedRuns(summary *BatchSummary) string {
	var lines []string
	for _, r := range summary.Runs {
		if r.Outcome != "passed" && r.Outcome != "skipped" {
			lines = append(lines, fmt.Sprintf("%s [%s]: %s %s", r.PlanName, r.Permutation, r.Outcome, r.Error))
		}
	}
	return strings.Join(lines, "\n")
}
