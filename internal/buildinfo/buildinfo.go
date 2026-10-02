// Package buildinfo holds values embedded in release binaries by the linker.
package buildinfo

import (
	"fmt"
	"strings"
)

// These variables may be set with -ldflags at release build time.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// String returns a concise, human-readable build identifier.
func String() string {
	version := Version
	if version == "" {
		version = "dev"
	}

	var details []string
	if Commit != "" && Commit != "unknown" {
		details = append(details, "commit "+Commit)
	}
	if BuildDate != "" && BuildDate != "unknown" {
		details = append(details, "built "+BuildDate)
	}
	if len(details) == 0 {
		return "yapp " + version
	}
	return fmt.Sprintf("yapp %s (%s)", version, strings.Join(details, ", "))
}
