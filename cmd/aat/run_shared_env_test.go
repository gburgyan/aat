package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gburgyan/aat/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envSelectCommand returns a command with the flags selectEnvName reads, parsed
// from args.
func envSelectCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("env", "", "")
	cmd.Flags().String("env-config", "", "")
	require.NoError(t, cmd.Flags().Parse(args))
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

func TestSelectEnvName(t *testing.T) {
	dir := t.TempDir()
	multi := filepath.Join(dir, "multi.yaml")
	require.NoError(t, os.WriteFile(multi, []byte("environments:\n  us:\n    apiBaseUrl: http://us\n  eu:\n    apiBaseUrl: http://eu\n"), 0o644))
	single := filepath.Join(dir, "single.yaml")
	require.NoError(t, os.WriteFile(single, []byte("apiBaseUrl: http://api\n"), 0o644))
	overlay := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(overlay, []byte("environment: eu\n"), 0o644))

	// The manifest's defaultEnvironment is "us" in every case.
	tests := []struct {
		name    string
		args    []string // --env and --env-config as given on the command line
		envFile string   // the environment file the run loads
		envVar  string   // AAT_ENV_NAME
		overlay string
		want    string
	}{
		{name: "manifest default", envFile: multi, want: "us"},
		{name: "--env beats every default", args: []string{"--env", "eu"}, envFile: multi, envVar: "us", overlay: overlay, want: "eu"},
		{name: "AAT_ENV_NAME beats the overlay", envFile: multi, envVar: "us", overlay: overlay, want: "us"},
		{name: "overlay beats the manifest default", envFile: multi, overlay: overlay, want: "eu"},
		{name: "--env-config does not take the manifest default", args: []string{"--env-config", multi}, envFile: multi, want: ""},
		{name: "--env-config with AAT_ENV_NAME", args: []string{"--env-config", multi}, envFile: multi, envVar: "eu", want: "eu"},
		{name: "single-environment file ignores the defaults", args: []string{"--env-config", single}, envFile: single, envVar: "us", overlay: overlay, want: ""},
		{name: "single-environment file ignores the manifest default", envFile: single, want: ""},
		{name: "single-environment file still gets an explicit --env", args: []string{"--env", "us"}, envFile: single, want: "us"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AAT_ENV_NAME", tt.envVar)
			cmd := envSelectCommand(t, tt.args...)
			resolved := &config.ProjectPaths{EnvPath: tt.envFile, DefaultEnvName: "us"}
			got, err := selectEnvName(cmd, resolved, tt.overlay, true)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
