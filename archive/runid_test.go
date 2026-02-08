package archive

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRunID_Format(t *testing.T) {
	id := GenerateRunID()
	// Expected format: run-YYYYMMDD-HHMMSS-XXXXXXXX (8 hex chars)
	pattern := `^run-\d{8}-\d{6}-[0-9a-f]{8}$`
	matched, err := regexp.MatchString(pattern, id)
	require.NoError(t, err)
	assert.True(t, matched, "RunID %q does not match pattern %s", id, pattern)
}

func TestGenerateRunID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateRunID()
		assert.False(t, seen[id], "duplicate RunID: %s", id)
		seen[id] = true
	}
}

func TestGenerateRunID_Prefix(t *testing.T) {
	id := GenerateRunID()
	assert.Equal(t, "run-", id[:4])
}
