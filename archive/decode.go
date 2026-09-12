package archive

import (
	"bytes"
	"encoding/json"
)

// legacyStepDurations holds what the current types do not read from an archive
// written before 0.1.0: each step's duration, recorded as duration_ms instead
// of durationMs.
type legacyStepDurations struct {
	Steps   []legacyStepDuration `json:"steps"`
	Cleanup []legacyStepDuration `json:"cleanup"`
}

type legacyStepDuration struct {
	DurationMs *int64 `json:"duration_ms"`
}

// decodeArchive decodes archive JSON, including archives written before 0.1.0.
// The legacy key is read in a second pass rather than by a StepRecord
// UnmarshalJSON: a custom unmarshaler decodes its fields without the caller's
// decoder settings, which would turn the exact numbers Redact keeps
// (json.Decoder.UseNumber) back into float64.
func decodeArchive(data []byte) (*Archive, error) {
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	if !bytes.Contains(data, []byte(`"duration_ms"`)) {
		return &a, nil
	}
	var legacy legacyStepDurations
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	fillLegacyDurations(a.Steps, legacy.Steps)
	fillLegacyDurations(a.Cleanup, legacy.Cleanup)
	return &a, nil
}

// fillLegacyDurations sets the duration of each record that has none from the
// duration_ms recorded at the same position.
func fillLegacyDurations(records []StepRecord, legacy []legacyStepDuration) {
	for i := range records {
		if i < len(legacy) && legacy[i].DurationMs != nil && records[i].DurationMs == 0 {
			records[i].DurationMs = *legacy[i].DurationMs
		}
	}
}
