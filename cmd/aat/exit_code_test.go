package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMainEnv makes the test binary run the aat CLI instead of its tests, so
// TestExitCodes can check the exit codes of real invocations, os.Exit included.
const runMainEnv = "AAT_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		os.Args = append([]string{"aat"}, os.Args[1:]...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runAAT runs the aat CLI with args in a child process started in dir. The
// child sees no project, user config, or environment name from the machine
// running the tests.
func runAAT(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	home := t.TempDir()

	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		runMainEnv+"=1",
		"HOME="+home,
		"XDG_CONFIG_HOME="+home,
		"AAT_PROJECT=",
		"AAT_ENV_NAME=",
	)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), out.String(), errOut.String()
	}
	require.NoError(t, err)
	return 0, out.String(), errOut.String()
}

// TestExitCodes checks the exit-code contract across commands: 0 when the
// command did what was asked, 1 when validation found a problem, and 2 when aat
// could not do what was asked. Run outcomes (1, 2, and 130) are covered by the
// exitCode and batchExitCode tests.
func TestExitCodes(t *testing.T) {
	empty := t.TempDir()

	// A project whose manifest fails to load (a misspelled key), and a file to import.
	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, "aat-project.yaml"),
		[]byte("name: typos\ngraph: graph.yaml\ntemplates: templates/\nplan: plans/\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "run.aar"), []byte("not a zip"), 0o644))
	const brokenManifest = `unknown key "plan"`

	shopManifest, err := filepath.Abs(filepath.Join("..", "..", "examples", "shop", "aat-project.yaml"))
	require.NoError(t, err)
	petstoreSpec, err := filepath.Abs(filepath.Join("testdata", "oas", "petstore.yaml"))
	require.NoError(t, err)

	// A project whose graph names an OpenAPI spec that doesn't exist.
	noSpec := t.TempDir()
	for name, content := range map[string]string{
		"aat-project.yaml": "name: nospec\ngraph: graph.yaml\ntemplates: templates/\nplans: plans/\nenvironment: env.yaml\n",
		"graph.yaml": "version: \"1.0.0\"\noas: missing.yaml\nnodes:\n  getOrder:\n    description: Get an order\n" +
			"    adapter: getOrder\n    oas:\n      operationId: getOrder\n    outputs:\n      - name: orderId\n        type: string\n",
		"templates/getOrder.yaml": "adapter: getOrder\nprotocol: http\nrequest:\n  method: GET\n  path: /orders/1\n" +
			"response:\n  extract:\n    orderId: \"$.orderId\"\n",
		"env.yaml":             "environment: test\napiBaseUrl: http://127.0.0.1:9\nauth:\n  type: none\n",
		"plans/get-order.yaml": "execution:\n  steps:\n    - node: getOrder\n",
	} {
		path := filepath.Join(noSpec, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	tests := []struct {
		name   string
		dir    string
		args   []string
		code   int
		stderr string // substring of stderr, when set
		stdout string // substring of stdout, when set
	}{
		{name: "help", dir: empty, code: 0},
		{name: "version", dir: empty, args: []string{"--version"}, code: 0},
		{name: "command group without a subcommand shows help", dir: empty, args: []string{"run"}, code: 0, stdout: "Available Commands"},

		{name: "unknown command", dir: empty, args: []string{"bogus"}, code: 2, stderr: `unknown command "bogus" for "aat"`},
		{name: "unknown subcommand", dir: empty, args: []string{"run", "bach"}, code: 2, stderr: "unknown command \"bach\" for \"aat run\"\n\nDid you mean this?\n\tbatch"},
		{name: "unknown subcommand suggests one", dir: empty, args: []string{"mcp", "serv"}, code: 2, stderr: "Did you mean this?\n\tserve"},
		{name: "unknown flag", dir: empty, args: []string{"run", "plan", "smoke", "--no-such-flag"}, code: 2, stderr: "unknown flag: --no-such-flag"},
		{name: "missing argument", dir: empty, args: []string{"run", "plan"}, code: 2, stderr: "accepts 1 arg(s)"},
		{name: "stray argument", dir: empty, args: []string{"validate", "extra"}, code: 2, stderr: `unknown command "extra" for "aat validate"`},
		{name: "execution flag on run clean", dir: empty, args: []string{"run", "clean", "--var", "a=b"}, code: 2, stderr: "unknown flag: --var"},

		{name: "run plan without a project", dir: empty, args: []string{"run", "plan", "smoke"}, code: 2},
		{name: "run plan with a manifest that fails to load", dir: broken, args: []string{"run", "plan", "smoke"}, code: 2, stderr: brokenManifest},
		{name: "run plan --json with a manifest that fails to load", dir: broken, args: []string{"run", "plan", "smoke", "--json"}, code: 2, stdout: `"outcome": "error"`},
		{name: "run plan --dump-state-secrets without --dump-state", dir: empty, args: []string{"run", "plan", "smoke", "--dump-state-secrets"}, code: 2, stderr: "--dump-state-secrets requires --dump-state"},
		{name: "run plan --oas-validate strict with a spec that doesn't load", dir: noSpec, args: []string{"run", "plan", "get-order", "--oas-validate", "strict"}, code: 2, stderr: "strict OAS validation"},
		{name: "run plan --json --oas-validate strict with a spec that doesn't load", dir: noSpec, args: []string{"run", "plan", "get-order", "--oas-validate", "strict", "--json"}, code: 2, stdout: `"outcome": "error"`},
		{name: "run show --shape without --step", dir: empty, args: []string{"run", "show", "latest", "--shape"}, code: 2, stderr: "--shape needs --step"},
		{name: "run show two parts", dir: empty, args: []string{"run", "show", "latest", "--step", "checkout", "--request", "--response"}, code: 2, stderr: "choose one part"},
		{name: "run show an unknown run", dir: empty, args: []string{"run", "show", "run-missing"}, code: 2, stderr: "run not found"},
		{name: "run batch with a bad --var", dir: empty, args: []string{"run", "batch", "--var", "novalue"}, code: 2},
		{name: "run batch --json with a bad --var", dir: empty, args: []string{"run", "batch", "--var", "novalue", "--json"}, code: 2, stdout: `"error": "`},

		{name: "validate without a manifest", dir: empty, args: []string{"validate"}, code: 2, stdout: "no manifest found"},
		{name: "validate with a bad --var", dir: empty, args: []string{"validate", "--var", "novalue"}, code: 2},
		{name: "validate with a --var the environment file never uses", dir: empty, args: []string{"validate", "--manifest", shopManifest, "--var", "nope=1"}, code: 2, stdout: "unknown var(s) nope"},
		{name: "validate finds a manifest that fails to load", dir: broken, args: []string{"validate"}, code: 1, stdout: brokenManifest},
		{name: "validate graph without a graph", dir: empty, args: []string{"validate", "graph"}, code: 2, stderr: "--graph is required"},
		{name: "generate with an --operation the spec lacks", dir: empty, args: []string{"generate", "--oas", petstoreSpec, "--operation", "nope", "--output-graph", "-"}, code: 2, stderr: `operationId "nope" is not in the spec`},

		{name: "plan list with a manifest that fails to load", dir: broken, args: []string{"plan", "list"}, code: 2, stderr: brokenManifest},
		{name: "mcp serve without a manifest", dir: empty, args: []string{"mcp", "serve"}, code: 2, stderr: "no manifest found"},
		{name: "mcp serve with a manifest that fails to load", dir: broken, args: []string{"mcp", "serve"}, code: 2, stderr: brokenManifest},
		{name: "import with a manifest that fails to load", dir: broken, args: []string{"import", "run.aar"}, code: 2, stderr: brokenManifest},
		{name: "import with a name outside the archive directory", dir: broken, args: []string{"import", "run.aar", "--name", "../escaped"}, code: 2, stderr: "invalid name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := runAAT(t, tt.dir, tt.args...)
			assert.Equal(t, tt.code, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			if tt.stderr != "" {
				assert.Contains(t, stderr, tt.stderr)
			}
			if tt.stdout != "" {
				assert.Contains(t, stdout, tt.stdout)
			}
		})
	}
}
