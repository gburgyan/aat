package graph

import (
	"fmt"
)

// Version represents a semantic version (Major.Minor.Patch).
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion parses a "X.Y.Z" semver string into a Version.
func ParseVersion(s string) (Version, error) {
	var v Version
	n, err := fmt.Sscanf(s, "%d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	if err != nil || n != 3 {
		return Version{}, fmt.Errorf("invalid version %q: expected format X.Y.Z", s)
	}
	// Reject trailing characters by round-tripping
	if v.String() != s {
		return Version{}, fmt.Errorf("invalid version %q: expected format X.Y.Z", s)
	}
	if v.Major < 0 || v.Minor < 0 || v.Patch < 0 {
		return Version{}, fmt.Errorf("invalid version %q: components must be non-negative", s)
	}
	return v, nil
}

// String returns the version in "X.Y.Z" format.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compatibility describes how compatible a plan version is with a graph version.
type Compatibility int

const (
	// VersionCompatible means the plan is fully compatible (same major, plan minor <= graph minor).
	VersionCompatible Compatibility = iota
	// VersionMinorDrift means same major but graph has a lower minor than plan (plan may use features not in graph).
	VersionMinorDrift
	// VersionIncompatible means different major versions — plan is likely incompatible.
	VersionIncompatible
)

// CheckCompatibility checks whether a plan generated against planVersion
// is compatible with the current graphVersion.
func CheckCompatibility(planVersion, graphVersion Version) Compatibility {
	if planVersion.Major != graphVersion.Major {
		return VersionIncompatible
	}
	if planVersion.Minor > graphVersion.Minor {
		return VersionMinorDrift
	}
	return VersionCompatible
}
