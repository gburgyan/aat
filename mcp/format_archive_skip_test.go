package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gburgyan/aat/archive"
)

func TestFormatArchiveDetail_CleanupSkipped(t *testing.T) {
	a := &archive.Archive{
		Result: archive.ArchiveResult{Outcome: "passed"},
		CleanupSkipped: []archive.CleanupSkipRecord{
			{Node: "voidPayment", CleanupFor: "createPayment", Reason: "when", When: `status == "authorized"`},
			{Node: "cancelOrder", CleanupFor: "createOrder", Reason: "released", ReleasedBy: "cancelOrder"},
		},
	}

	out := formatArchiveDetail(a)
	assert.Contains(t, out, "## Cleanup Skipped\n\n")
	assert.Contains(t, out, `- **voidPayment** for createPayment: when status == "authorized" is false`)
	assert.Contains(t, out, "- **cancelOrder** for createOrder: released by cancelOrder")
}
