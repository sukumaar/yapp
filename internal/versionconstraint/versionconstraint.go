// Package versionconstraint matches semantic versions against npm-style ranges.
package versionconstraint

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ValidateVersion requires a full SemVer version, without coercing release IDs.
func ValidateVersion(version string) error {
	_, err := semver.StrictNewVersion(version)
	return err
}

// Match supports exact versions, comparisons, caret, tilde, wildcards,
// hyphen ranges, AND (spaces or commas), and OR (||).
func Match(version, constraint string) (bool, error) {
	if strings.TrimSpace(constraint) == "" {
		return false, fmt.Errorf("version constraint must not be empty")
	}
	// The underlying library accepts reversed comparator aliases. Keep the
	// conventional npm-style spellings >= and <= at the catalog boundary.
	if strings.Contains(constraint, "=>") || strings.Contains(constraint, "=<") {
		return false, fmt.Errorf("invalid version constraint %q: use >= or <=", constraint)
	}
	rangeSet, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	parsed, err := semver.StrictNewVersion(version)
	if err != nil {
		return false, fmt.Errorf("version %q is not SemVer; declare semantic_version explicitly: %w", version, err)
	}
	return rangeSet.Check(parsed), nil
}
