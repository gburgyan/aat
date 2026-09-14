package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFindRun writes a run's archive.json, and the summary.json Write adds,
// at rel under dir, with ts as the run's timestamp.
func writeFindRun(t *testing.T, dir, rel string, ts time.Time) {
	t.Helper()
	a := &Archive{
		Metadata: ArchiveMetadata{RunID: filepath.Base(rel), Timestamp: ts},
		Result:   ArchiveResult{Outcome: "passed"},
	}
	require.NoError(t, Write(a, filepath.Join(dir, rel, "archive.json")))
}

func at(hour int) time.Time {
	return time.Date(2026, 9, 12, hour, 0, 0, 0, time.UTC)
}

func TestListRuns(t *testing.T) {
	dir := t.TempDir()
	writeFindRun(t, dir, "run-20260912-100000-aaaa0001", at(10))
	writeFindRun(t, dir, "batch-20260912-110000-bbbb0001/run-20260912-110001-aaaa0002", at(11))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "batch-20260912-110000-bbbb0001", "batch.json"), []byte(`{}`), 0o644))
	writeFindRun(t, dir, "!nightly/run-20260912-120001-aaaa0003", at(12))
	// A batch that is still going, or stopped before writing batch.json.
	writeFindRun(t, dir, "batch-20260912-130000-bbbb0002/run-20260912-130001-aaaa0004", at(13))
	// Its next run has not written an archive yet.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "batch-20260912-130000-bbbb0002", "run-20260912-130002-aaaa0005"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a run"), 0o644))

	runs, err := ListRuns(dir)
	require.NoError(t, err)
	refs := make([]string, 0, len(runs))
	for _, r := range runs {
		refs = append(refs, r.Ref())
	}
	assert.Equal(t, []string{
		"batch-20260912-130000-bbbb0002/run-20260912-130001-aaaa0004",
		"!nightly/run-20260912-120001-aaaa0003",
		"batch-20260912-110000-bbbb0001/run-20260912-110001-aaaa0002",
		"run-20260912-100000-aaaa0001",
	}, refs)
	assert.Equal(t, "batch-20260912-130000-bbbb0002", runs[0].BatchID)
	assert.Equal(t, "run-20260912-130001-aaaa0004", runs[0].ID)
	assert.Equal(t, filepath.Join(dir, "run-20260912-100000-aaaa0001", "archive.json"), runs[3].ArchivePath)
}

// TestListRuns_WithoutSummary: a run without summary.json is ordered by the
// timestamp in its directory name, and listing does not write the summary.
func TestListRuns_WithoutSummary(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"run-20260912-090000-aaaa0002", "run-20260912-100000-aaaa0001"} {
		writeFindRun(t, dir, id, time.Time{})
		require.NoError(t, os.Remove(filepath.Join(dir, id, "summary.json")))
	}

	runs, err := ListRuns(dir)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "run-20260912-100000-aaaa0001", runs[0].ID)
	assert.NoFileExists(t, filepath.Join(dir, runs[0].ID, "summary.json"))
}

func TestListRuns_MissingDir(t *testing.T) {
	runs, err := ListRuns(filepath.Join(t.TempDir(), "none"))
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestFindRun(t *testing.T) {
	dir := t.TempDir()
	const batchRun = "batch-20260912-110000-bbbb0001/run-20260912-110001-aaaa0002"
	writeFindRun(t, dir, "run-20260912-100000-aaaa0001", at(10))
	writeFindRun(t, dir, batchRun, at(11))

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "latest", want: batchRun},
		{ref: "run-20260912-100000-aaaa0001", want: "run-20260912-100000-aaaa0001"},
		{ref: "run-20260912-110001-aaaa0002", want: batchRun},
		{ref: batchRun, want: batchRun},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			run, err := FindRun(dir, tt.ref)
			require.NoError(t, err)
			assert.Equal(t, tt.want, run.Ref())
			assert.FileExists(t, run.ArchivePath)
		})
	}

	for _, ref := range []string{
		"",
		"run-missing",
		"batch-20260912-110000-bbbb0001/run-missing",
		"..",
		"../run-20260912-100000-aaaa0001",
		"batch-20260912-110000-bbbb0001/../run-20260912-100000-aaaa0001",
		"batch-20260912-110000-bbbb0001/run-20260912-110001-aaaa0002/archive.json",
	} {
		_, err := FindRun(dir, ref)
		assert.ErrorIs(t, err, ErrRunNotFound, "ref %q", ref)
	}
}

// TestFindRun_PlanInBatch resolves a batch ID and a plan name, as batch.json
// records it, to the plan's run in the batch.
func TestFindRun_PlanInBatch(t *testing.T) {
	dir := t.TempDir()
	const batch = "batch-20260912-110000-bbbb0001"
	const (
		smokeRun    = "run-20260912-110001-aaaa0001"
		standardRun = "run-20260912-110002-aaaa0002"
		expressRun  = "run-20260912-110003-aaaa0003"
		oddRun      = "run-20260912-110004-aaaa0004"
		fullRun     = "run-20260912-110005-aaaa0005"
	)
	for _, id := range []string{smokeRun, standardRun, expressRun, oddRun, fullRun} {
		writeFindRun(t, dir, batch+"/"+id, at(11))
	}
	writeFindRun(t, dir, "run-20260912-100000-aaaa0009", at(10))
	b := &BatchArchive{
		Metadata: BatchMetadata{BatchID: batch},
		Runs: []BatchRunEntry{
			{PlanName: "smoke", RunID: smokeRun, Outcome: "passed"},
			{PlanName: "negative/state-machine", RunID: standardRun, Outcome: "passed", Layers: []string{"shipping-standard"}},
			{PlanName: "negative/state-machine", RunID: expressRun, Outcome: "failed", Layers: []string{"shipping-express"}},
			{PlanName: "full-lifecycle", RunID: fullRun, Outcome: "passed", Layers: []string{"shipping-standard"}},
			{PlanName: "full-lifecycle", Outcome: "skipped", Layers: []string{"shipping-express"}, Skipped: true, DuplicateOf: "full-lifecycle [shipping-standard]"},
			// A plan named like another run's ID.
			{PlanName: smokeRun, RunID: oddRun, Outcome: "passed"},
			// A run whose directory holds no archive, and one outside the batch.
			{PlanName: "resilience", RunID: "run-20260912-110006-aaaa0006", Outcome: "error"},
			{PlanName: "escape", RunID: "../run-20260912-100000-aaaa0009", Outcome: "passed"},
		},
	}
	require.NoError(t, WriteBatch(b, filepath.Join(dir, batch, "batch.json")))

	tests := []struct {
		name      string
		ref       string
		want      string   // the run's reference, when one is found
		ambiguous []string // the run IDs an ambiguous reference lists
		notFound  bool
	}{
		{name: "a plan with one run", ref: batch + "/smoke", want: batch + "/" + smokeRun},
		{name: "a skipped duplicate doesn't count", ref: batch + "/full-lifecycle", want: batch + "/" + fullRun},
		{name: "a run ID wins over a plan name", ref: batch + "/" + smokeRun, want: batch + "/" + smokeRun},
		{name: "a plan with several runs", ref: batch + "/negative/state-machine", ambiguous: []string{standardRun, expressRun}},
		{name: "no such plan", ref: batch + "/checkout", notFound: true},
		{name: "a plan whose run has no archive", ref: batch + "/resilience", notFound: true},
		{name: "a run outside the batch", ref: batch + "/escape", notFound: true},
		{name: "an empty plan name", ref: batch + "/", notFound: true},
		{name: "a batch without batch.json", ref: "batch-20260912-120000-bbbb0002/smoke", notFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run, err := FindRun(dir, tt.ref)
			switch {
			case tt.notFound:
				assert.ErrorIs(t, err, ErrRunNotFound)
			case tt.ambiguous != nil:
				var ambiguous *AmbiguousRunError
				require.ErrorAs(t, err, &ambiguous)
				assert.ErrorIs(t, err, ErrAmbiguousRun)
				ids := make([]string, 0, len(ambiguous.Runs))
				for _, r := range ambiguous.Runs {
					ids = append(ids, r.RunID)
				}
				assert.Equal(t, tt.ambiguous, ids)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, run.Ref())
				assert.FileExists(t, run.ArchivePath)
			}
		})
	}

	_, err := FindRun(dir, batch+"/negative/state-machine")
	assert.EqualError(t, err, `plan "negative/state-machine" ran as 2 runs in batch `+batch+`; name one of them:
  `+batch+`/`+standardRun+`  layers shipping-standard
  `+batch+`/`+expressRun+`  layers shipping-express`)
}

func TestFindRun_LatestWithoutRuns(t *testing.T) {
	_, err := FindRun(t.TempDir(), "latest")
	assert.ErrorIs(t, err, ErrRunNotFound)
}
