package plan

import (
	"crypto/sha256"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Fingerprint returns a SHA-256 hex digest of the plan's Execution section.
// Only Execution is hashed — Metadata, Intent, Auth, and Headers don't vary
// by layer permutation. yaml.v3 sorts map keys deterministically, ensuring
// identical execution content produces the same fingerprint.
// Returns an empty string for nil plans.
func Fingerprint(p *Plan) string {
	if p == nil {
		return ""
	}
	data, err := yaml.Marshal(p.Execution)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
