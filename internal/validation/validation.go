// Package validation checks declarative configuration values.
package validation

import (
	"path/filepath"
	"strings"
)

func SafeRelativePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, `\`) {
		return false
	}
	for _, component := range strings.Split(filepath.ToSlash(value), "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
		for _, char := range component {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._+-", char)) {
				return false
			}
		}
	}
	return true
}

func EnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_' || index > 0 && char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}
