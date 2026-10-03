// Package state manages YAPP's per-user installation registry.
package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sukumaar/yapp/internal/fileutil"
	"github.com/sukumaar/yapp/internal/validation"
	"github.com/sukumaar/yapp/internal/versionconstraint"
)

const schemaVersion = 1
const maxStateSize = 1 << 20

// State is the local registry stored in ~/.yapp/.yapp_config.
type State struct {
	SchemaVersion int                `json:"schemaVersion"`
	Apps          map[string]Install `json:"apps"`
}

// Install records a YAPP-managed app installation.
type Install struct {
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	SemanticVersion string    `json:"semanticVersion,omitempty"`
	Path            string    `json:"path"`
	ArtifactURL     string    `json:"artifactUrl"`
	SHA256          string    `json:"sha256"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	InstalledAt     time.Time `json:"installedAt"`
	LinkedBinaries  []string  `json:"linkedBinaries,omitempty"`
}

// Load reads the local registry, returning an empty state when it does not exist.
func Load(home string) (State, error) {
	if info, err := os.Lstat(home); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return State{}, fmt.Errorf("YAPP home must be a real directory: %s", home)
		}
	} else if !os.IsNotExist(err) {
		return State{}, fmt.Errorf("inspect YAPP home: %w", err)
	}
	configPath := filepath.Join(home, ".yapp_config")
	info, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		return State{SchemaVersion: schemaVersion, Apps: make(map[string]Install)}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("inspect local state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return State{}, fmt.Errorf("local state must be a regular file: %s", configPath)
	}
	if info.Size() > maxStateSize {
		return State{}, fmt.Errorf("local state exceeds the maximum size of %d bytes", maxStateSize)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return State{}, fmt.Errorf("read local state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result State
	if err := decoder.Decode(&result); err != nil {
		return State{}, fmt.Errorf("decode local state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return State{}, fmt.Errorf("decode local state: multiple JSON values")
		}
		return State{}, fmt.Errorf("decode local state: %w", err)
	}
	if result.SchemaVersion != schemaVersion {
		return State{}, fmt.Errorf("unsupported local state schema version %d", result.SchemaVersion)
	}
	if result.Apps == nil {
		result.Apps = make(map[string]Install)
	}
	for appID, install := range result.Apps {
		for _, binary := range install.LinkedBinaries {
			if !validation.SafeRelativePath(binary) {
				return State{}, fmt.Errorf("unsafe linked binary for %q", appID)
			}
		}
		if install.SemanticVersion != "" {
			if err := versionconstraint.ValidateVersion(install.SemanticVersion); err != nil {
				return State{}, fmt.Errorf("invalid recorded semantic version for %q: %w", appID, err)
			}
		}
		if strings.TrimSpace(appID) == "" || strings.ContainsAny(appID, `/\\`) || appID == "." || appID == ".." {
			return State{}, fmt.Errorf("local state has invalid app identifier %q", appID)
		}
		if install.Name == "" || install.Version == "" || !validation.SafeRelativePath(install.Path) || !validSHA256(install.SHA256) || install.ArtifactURL == "" || install.OS == "" || install.Arch == "" {
			return State{}, fmt.Errorf("local state has incomplete or invalid installation data for %q", appID)
		}
	}
	return result, nil
}

// Record adds or updates an installed app in the local registry.
func Record(home, appID string, install Install) error {
	for _, binary := range install.LinkedBinaries {
		if !validation.SafeRelativePath(binary) {
			return fmt.Errorf("unsafe linked binary %q", binary)
		}
	}
	if install.SemanticVersion != "" {
		if err := versionconstraint.ValidateVersion(install.SemanticVersion); err != nil {
			return fmt.Errorf("invalid semantic version: %w", err)
		}
	}
	if strings.TrimSpace(appID) == "" || strings.ContainsAny(appID, `/\\`) || appID == "." || appID == ".." {
		return fmt.Errorf("invalid app identifier %q", appID)
	}
	if install.Name == "" || install.Version == "" || !validation.SafeRelativePath(install.Path) || !validSHA256(install.SHA256) || install.ArtifactURL == "" || install.OS == "" || install.Arch == "" {
		return fmt.Errorf("incomplete installation record for %q", appID)
	}
	if err := ensurePrivateDirectory(home); err != nil {
		return err
	}
	current, err := Load(home)
	if err != nil {
		return err
	}
	current.Apps[appID] = install
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local state: %w", err)
	}
	data = append(data, '\n')
	if err := fileutil.AtomicWrite(filepath.Join(home, ".yapp_config"), data, 0o600); err != nil {
		return fmt.Errorf("write local state: %w", err)
	}
	return nil
}

// Remove deletes an app from the local installation registry.
func Remove(home, appID string) error {
	if strings.TrimSpace(appID) == "" || strings.ContainsAny(appID, `/\\`) || appID == "." || appID == ".." {
		return fmt.Errorf("invalid app identifier %q", appID)
	}
	current, err := Load(home)
	if err != nil {
		return err
	}
	if _, exists := current.Apps[appID]; !exists {
		return fmt.Errorf("app %q is not recorded as installed", appID)
	}
	delete(current.Apps, appID)
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local state: %w", err)
	}
	data = append(data, '\n')
	if err := fileutil.AtomicWrite(filepath.Join(home, ".yapp_config"), data, 0o600); err != nil {
		return fmt.Errorf("write local state: %w", err)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("YAPP home must be a real directory: %s", path)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("secure YAPP home permissions: %w", err)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect YAPP home: %w", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create YAPP home: %w", err)
	}
	return ensurePrivateDirectory(path)
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
