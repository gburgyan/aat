package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunShow_Compact(t *testing.T) {
	dir := t.TempDir()
	writeShowArchive(t, dir, showRunID, showTestArchive())

	oneLine := func(t *testing.T, out string) {
		t.Helper()
		assert.Equal(t, 1, strings.Count(out, "\n"), "one line: %q", out)
		assert.True(t, strings.HasSuffix(out, "\n"), out)
	}

	t.Run("part", func(t *testing.T) {
		out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Compact: true})
		require.NoError(t, err)
		oneLine(t, out)
		assert.Contains(t, out, `"lines":[{"sku":"SKU-1","qty":1}`)
	})

	t.Run("part with a path", func(t *testing.T) {
		out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Path: "lines.#.sku", Compact: true})
		require.NoError(t, err)
		assert.Equal(t, "[\"SKU-1\",\"SKU-2\"]\n", out)
	})

	t.Run("step list", func(t *testing.T) {
		out, _, err := runShow(t, dir, "latest", showOptions{JSON: true, Compact: true})
		require.NoError(t, err)
		oneLine(t, out)
		var list shownRunList
		require.NoError(t, json.Unmarshal([]byte(out), &list))
		assert.Len(t, list.Steps, 3)
	})

	t.Run("step", func(t *testing.T) {
		out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", JSON: true, Compact: true})
		require.NoError(t, err)
		oneLine(t, out)
		assert.Contains(t, out, `"url":"http://localhost:8765/us/v1/carts/cart_0001/checkout"`, "& and / stay unescaped")
	})

	t.Run("shape", func(t *testing.T) {
		out, _, err := runShow(t, dir, "latest", showOptions{Step: "checkout", Part: "response", Shape: true, JSON: true, Compact: true})
		require.NoError(t, err)
		oneLine(t, out)
		assert.True(t, strings.HasPrefix(out, "[{"), out)
	})
}

func TestCheckCompact(t *testing.T) {
	tests := []struct {
		name    string
		opts    showOptions
		wantErr string
	}{
		{name: "not set", opts: showOptions{}},
		{name: "step list JSON", opts: showOptions{JSON: true, Compact: true}},
		{name: "part", opts: showOptions{Step: "checkout", Part: "response", Compact: true}},
		{name: "shape JSON", opts: showOptions{Step: "checkout", Part: "response", Shape: true, JSON: true, Compact: true}},
		{name: "step list text", opts: showOptions{Compact: true}, wantErr: "--compact needs --json, or a part flag"},
		{name: "step text", opts: showOptions{Step: "checkout", Compact: true}, wantErr: "--compact needs --json, or a part flag"},
		{name: "shape text", opts: showOptions{Step: "checkout", Part: "response", Shape: true, Compact: true}, wantErr: "--compact --shape needs --json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCompact(tt.opts)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
