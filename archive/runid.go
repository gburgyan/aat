package archive

import (
	"crypto/rand"
	"fmt"
	"time"
)

// GenerateRunID returns a unique run identifier in the format "run-YYYYMMDD-HHMMSS-XXXXXXXX"
// where XXXXXXXX is an 8-character hex suffix from crypto/rand.
func GenerateRunID() string {
	now := time.Now()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix) // crypto/rand.Read never returns an error on supported platforms
	return fmt.Sprintf("run-%s-%08x", now.Format("20060102-150405"), suffix)
}

// GenerateBatchID returns a unique batch identifier in the format "batch-YYYYMMDD-HHMMSS-XXXXXXXX".
func GenerateBatchID() string {
	now := time.Now()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("batch-%s-%08x", now.Format("20060102-150405"), suffix)
}
