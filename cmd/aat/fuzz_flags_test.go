package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gburgyan/aat/engine"
)

func parseFuzzFlags(t *testing.T, args ...string) (*engine.FuzzConfig, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	addExecutionFlags(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return fuzzConfigFromFlags(cmd)
}

func TestFuzzConfigFromFlags(t *testing.T) {
	cfg, err := parseFuzzFlags(t)
	require.NoError(t, err)
	assert.Nil(t, cfg, "no --fuzz, no fuzzing")

	cfg, err = parseFuzzFlags(t, "--fuzz", "addItem,checkout", "--fuzz-mode", "negative", "--fuzz-cases", "10",
		"--fuzz-scope", "shared", "--fuzz-fail", "server-error,accepted-invalid", "--fuzz-case", "quantity.zero", "--fuzz-input", "quantity")
	require.NoError(t, err)
	assert.Equal(t, &engine.FuzzConfig{
		Targets: []string{"addItem", "checkout"},
		Modes:   []string{"negative"},
		Inputs:  []string{"quantity"},
		Max:     10,
		Shared:  true,
		Cases:   []string{"quantity.zero"},
		Fail:    []string{"server-error", "accepted-invalid"},
	}, cfg)

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--fuzz-mode", "negative"}, "--fuzz-mode needs --fuzz"},
		{[]string{"--fuzz", "a", "--fuzz-mode", "evil"}, "--fuzz-mode evil"},
		{[]string{"--fuzz", "a", "--fuzz-scope", "global"}, "--fuzz-scope global"},
		{[]string{"--fuzz", "a", "--fuzz-fail", "crash"}, `unknown finding "crash"`},
		{[]string{"--fuzz", "a", "--fuzz-cases", "-1"}, "must not be negative"},
	} {
		_, err := parseFuzzFlags(t, tt.args...)
		assert.ErrorContains(t, err, tt.want, tt.args)
	}
}
