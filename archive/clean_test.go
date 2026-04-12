package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"run prefix", "run-20260315-143000-abcd1234", false},
		{"batch prefix", "batch-20260315-143000-abcd1234", false},
		{"named dir", "my-saved-run", true},
		{"bang prefix", "!run-20260315-143000-abcd1234", true},
		{"too short run", "run-2026", true},
		{"too short batch", "batch-2026", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := ParseRunTimestamp(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 2026, ts.Year())
			assert.Equal(t, time.March, ts.Month())
			assert.Equal(t, 15, ts.Day())
			assert.Equal(t, 14, ts.Hour())
			assert.Equal(t, 30, ts.Minute())
		})
	}
}

func TestCleanOldRuns(t *testing.T) {
	dir := t.TempDir()

	// Helper to create a run directory with the given name.
	mkDir := func(name string) {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
	}

	// Create directories with timestamps at various ages.
	now := time.Now()
	oldDate := now.AddDate(0, 0, -10).Format("20060102-150405")
	recentDate := now.AddDate(0, 0, -2).Format("20060102-150405")

	oldRun := "run-" + oldDate + "-aaaa0001"
	oldBatch := "batch-" + oldDate + "-bbbb0002"
	recentRun := "run-" + recentDate + "-cccc0003"
	recentBatch := "batch-" + recentDate + "-dddd0004"

	mkDir(oldRun)
	mkDir(oldBatch)
	mkDir(recentRun)
	mkDir(recentBatch)
	mkDir("my-saved-run")                  // named
	mkDir("!run-" + oldDate + "-eeee0005") // saved with bang prefix
	mkDir("!old-batch")                    // saved with bang prefix

	// Also create a regular file (should be ignored).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644))

	t.Run("dry run", func(t *testing.T) {
		opts := CleanOptions{
			MaxAge: 7 * 24 * time.Hour,
			DryRun: true,
		}
		result, err := CleanOldRuns(dir, opts, func(string, ...any) {})
		require.NoError(t, err)

		assert.Equal(t, 2, result.Deleted) // old run + old batch
		assert.Equal(t, 3, result.Skipped) // my-saved-run + 2 bang-prefixed
		assert.Equal(t, 0, result.Errors)

		// Directories should still exist (dry run).
		assert.DirExists(t, filepath.Join(dir, oldRun))
		assert.DirExists(t, filepath.Join(dir, oldBatch))
	})

	t.Run("actual delete", func(t *testing.T) {
		opts := CleanOptions{
			MaxAge: 7 * 24 * time.Hour,
			DryRun: false,
		}
		result, err := CleanOldRuns(dir, opts, func(string, ...any) {})
		require.NoError(t, err)

		assert.Equal(t, 2, result.Deleted)
		assert.Equal(t, 3, result.Skipped)
		assert.Equal(t, 0, result.Errors)

		// Old directories should be gone.
		assert.NoDirExists(t, filepath.Join(dir, oldRun))
		assert.NoDirExists(t, filepath.Join(dir, oldBatch))

		// Recent and saved directories should remain.
		assert.DirExists(t, filepath.Join(dir, recentRun))
		assert.DirExists(t, filepath.Join(dir, recentBatch))
		assert.DirExists(t, filepath.Join(dir, "my-saved-run"))
	})
}

func TestIsNamed(t *testing.T) {
	tests := []struct {
		name   string
		dir    string
		expect bool
	}{
		{"auto run", "run-20260101-120000-abcd1234", false},
		{"auto batch", "batch-20260101-120000-abcd1234", false},
		{"custom name", "my-test-run", true},
		{"bang prefix", "!run-20260101-120000-abcd1234", true},
		{"bang custom", "!my-test", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, IsNamed(tt.dir))
		})
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "< 1 day", formatAge(now, now.Add(-2*time.Hour)))
	assert.Equal(t, "1 day", formatAge(now, now.Add(-36*time.Hour)))
	assert.Equal(t, "5 days", formatAge(now, now.Add(-5*24*time.Hour)))
}
