// Package catalog loads and validates YAPP's embedded app catalog.
package catalog

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/sukumaar/yapp/internal/validation"
	"github.com/sukumaar/yapp/internal/versionconstraint"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var data []byte

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// Catalog contains apps keyed by canonical CLI identifier and aliases to those IDs.
type Catalog struct {
	Aliases map[string]string `yaml:"aliases"`
	Apps    map[string]App    `yaml:"apps"`
}

// App describes one pinned version of an app.
type App struct {
	Name            string            `yaml:"name"`
	Version         string            `yaml:"version"`
	SemanticVersion string            `yaml:"semantic_version"`
	ReleaseURL      string            `yaml:"release_url"`
	Artifacts       []Artifact        `yaml:"artifacts"`
	InstallPath     string            `yaml:"install_path"`
	Executables     []string          `yaml:"executables"`
	Environment     EnvironmentConfig `yaml:"environment"`
	DependsOn       []Dependency      `yaml:"depends_on"`
	InstallMode     *InstallMode      `yaml:"install_mode"`
}

// InstallMode selects the commands exposed through the shared YAPP bin.
// When present, per-app environment.paths are not added to PATH.
type InstallMode struct {
	Symlink  bool     `yaml:"symlink"`
	Binaries []string `yaml:"binaries"`
}

func (a App) LinkedBinaries() []string {
	if a.InstallMode != nil && a.InstallMode.Symlink {
		return a.InstallMode.Binaries
	}
	return nil
}

// Dependency identifies a package family and the version expected by this app.
type Dependency struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// MatchingVersion preserves the upstream release ID while allowing an explicit
// SemVer compatibility version for releases that do not use SemVer.
func (a App) MatchingVersion() string {
	if a.SemanticVersion != "" {
		return a.SemanticVersion
	}
	return a.Version
}

// EnvironmentConfig describes variables and PATH entries derived from an
// installation. Paths are relative to InstallPath; dependency apps are added to
// PATH before the apps that depend on them.
type EnvironmentConfig struct {
	Variables map[string]string `yaml:"variables"`
	Paths     []string          `yaml:"paths"`
}

// Artifact describes a downloadable archive for one platform.
type Artifact struct {
	OS              string `yaml:"os"`
	Arch            string `yaml:"arch"`
	Format          string `yaml:"format"`
	URL             string `yaml:"url"`
	SHA256          string `yaml:"sha256"`
	StripComponents int    `yaml:"strip_components"`
}

// Default loads the catalog distributed with the YAPP binary.
func Default() (Catalog, error) {
	return Parse(data)
}

// Parse decodes a catalog and rejects unknown fields or invalid entries.
func Parse(data []byte) (Catalog, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var result Catalog
	if err := decoder.Decode(&result); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Catalog{}, fmt.Errorf("decode catalog: multiple YAML documents are not supported")
		}
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	if err := result.Validate(); err != nil {
		return Catalog{}, err
	}
	return result, nil
}

// Validate checks catalog names, install paths, and artifact metadata.
func (c Catalog) Validate() error {
	if len(c.Apps) == 0 {
		return fmt.Errorf("catalog has no apps")
	}
	for id, app := range c.Apps {
		if !validIdentifier(id) {
			return fmt.Errorf("invalid app identifier %q", id)
		}
		if strings.TrimSpace(app.Name) == "" || strings.TrimSpace(app.Version) == "" {
			return fmt.Errorf("app %q must have a name and version", id)
		}
		if app.SemanticVersion != "" {
			if err := versionconstraint.ValidateVersion(app.SemanticVersion); err != nil {
				return fmt.Errorf("app %q has invalid semantic_version: %w", id, err)
			}
		}
		if app.ReleaseURL != "" {
			releaseURL, err := url.ParseRequestURI(app.ReleaseURL)
			if err != nil || releaseURL.Scheme != "https" || releaseURL.Host == "" || releaseURL.User != nil {
				return fmt.Errorf("app %q has an invalid or non-HTTPS release URL", id)
			}
		}
		if !validRelativePath(app.InstallPath) {
			return fmt.Errorf("app %q has an unsafe install_path", id)
		}
		if a := app.InstallMode; a != nil {
			if a.Symlink && len(a.Binaries) == 0 {
				return fmt.Errorf("app %q enables symlinks without binaries", id)
			}
			seen := make(map[string]bool)
			for _, binary := range a.Binaries {
				if !validRelativePath(binary) {
					return fmt.Errorf("app %q has unsafe install_mode binary %q", id, binary)
				}
				name := path.Base(binary)
				if seen[name] {
					return fmt.Errorf("app %q exposes duplicate command %q", id, name)
				}
				seen[name] = true
			}
		}
		if len(app.Executables) == 0 {
			return fmt.Errorf("app %q must specify at least one executable to verify", id)
		}
		for _, executable := range app.Executables {
			if !validRelativePath(executable) {
				return fmt.Errorf("app %q has an unsafe executable path", id)
			}
		}
		for name, value := range app.Environment.Variables {
			if !validation.EnvironmentName(name) || value != "." && !validRelativePath(value) {
				return fmt.Errorf("app %q has an invalid environment variable setting", id)
			}
		}
		for _, path := range app.Environment.Paths {
			if !validRelativePath(path) {
				return fmt.Errorf("app %q has an unsafe environment PATH entry", id)
			}
		}
		seenDependencies := make(map[string]struct{}, len(app.DependsOn))
		for _, dependency := range app.DependsOn {
			if !validIdentifier(dependency.Name) || strings.TrimSpace(dependency.Version) == "" {
				return fmt.Errorf("app %q has an invalid dependency %q", id, dependency.Name)
			}
			if _, exists := seenDependencies[dependency.Name]; exists {
				return fmt.Errorf("app %q lists dependency %q more than once", id, dependency.Name)
			}
			seenDependencies[dependency.Name] = struct{}{}
		}
		if len(app.Artifacts) == 0 {
			return fmt.Errorf("app %q has no artifacts", id)
		}
		seen := make(map[string]struct{}, len(app.Artifacts))
		for _, artifact := range app.Artifacts {
			if artifact.OS == "" || artifact.Arch == "" {
				return fmt.Errorf("app %q artifact must specify os and arch", id)
			}
			platform := artifact.OS + "/" + artifact.Arch
			if _, exists := seen[platform]; exists {
				return fmt.Errorf("app %q has duplicate artifact for %s", id, platform)
			}
			seen[platform] = struct{}{}
			if artifact.Format != "tar.gz" {
				return fmt.Errorf("app %q has unsupported archive format %q", id, artifact.Format)
			}
			parsedURL, err := url.ParseRequestURI(artifact.URL)
			if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil {
				return fmt.Errorf("app %q has an invalid or non-HTTPS artifact URL", id)
			}
			if !sha256Pattern.MatchString(artifact.SHA256) {
				return fmt.Errorf("app %q has an invalid SHA-256 checksum", id)
			}
			if artifact.StripComponents < 0 || artifact.StripComponents > 8 {
				return fmt.Errorf("app %q has invalid strip_components", id)
			}
		}
	}
	for alias, id := range c.Aliases {
		if !validIdentifier(alias) {
			return fmt.Errorf("invalid app alias %q", alias)
		}
		if _, exists := c.Apps[alias]; exists {
			return fmt.Errorf("app alias %q conflicts with a catalog identifier", alias)
		}
		if _, exists := c.Apps[id]; !exists {
			return fmt.Errorf("app alias %q targets unknown catalog identifier %q", alias, id)
		}
	}
	for id, app := range c.Apps {
		for _, dependency := range app.DependsOn {
			if _, _, err := c.ResolveDependency(dependency); err != nil {
				return fmt.Errorf("app %q dependency %q: %w", id, dependency.Name, err)
			}
		}
	}
	if err := c.validateDependencyGraph(); err != nil {
		return err
	}
	return nil
}

// ResolveApp finds an app by its catalog identifier or configured alias.
func (c Catalog) ResolveApp(id string) (string, App, bool) {
	if app, ok := c.Apps[id]; ok {
		return id, app, true
	}
	canonicalID, ok := c.Aliases[id]
	if !ok {
		return "", App{}, false
	}
	app, ok := c.Apps[canonicalID]
	return canonicalID, app, ok
}

// ResolveDependency finds the versioned catalog app that provides a dependency
// family and satisfies its version constraint.
func (c Catalog) ResolveDependency(dependency Dependency) (string, App, error) {
	ids := make([]string, 0, len(c.Apps))
	for id := range c.Apps {
		base := strings.SplitN(id, "@", 2)[0]
		if id == dependency.Name || base == dependency.Name {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var matches []string
	for _, id := range ids {
		app := c.Apps[id]
		ok, err := versionconstraint.Match(app.MatchingVersion(), dependency.Version)
		if err != nil {
			return "", App{}, err
		}
		if ok {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		if len(ids) == 0 {
			return "", App{}, fmt.Errorf("unknown app %q", dependency.Name)
		}
		return "", App{}, fmt.Errorf("no catalog version of %q satisfies %q", dependency.Name, dependency.Version)
	case 1:
		id := matches[0]
		return id, c.Apps[id], nil
	default:
		return "", App{}, fmt.Errorf("dependency %q version %q matches multiple catalog apps: %s", dependency.Name, dependency.Version, strings.Join(matches, ", "))
	}
}

func (c Catalog) validateDependencyGraph() error {
	visiting := make(map[string]bool, len(c.Apps))
	visited := make(map[string]bool, len(c.Apps))
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("catalog contains a dependency cycle involving %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range c.Apps[id].DependsOn {
			providerID, _, err := c.ResolveDependency(dependency)
			if err != nil {
				return err
			}
			if err := visit(providerID); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for id := range c.Apps {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// ArtifactFor selects the catalog artifact matching the current platform.
func (a App) ArtifactFor(osName, arch string) (Artifact, error) {
	for _, artifact := range a.Artifacts {
		if artifact.OS == osName && artifact.Arch == arch {
			return artifact, nil
		}
	}
	return Artifact{}, fmt.Errorf("%s has no artifact for %s/%s", a.Name, osName, arch)
}

// DependencyOrder returns installed app IDs with dependencies before dependents.
func (c Catalog) DependencyOrder(appIDs []string) []string {
	installed := make(map[string]bool, len(appIDs))
	for _, id := range appIDs {
		if _, exists := c.Apps[id]; exists {
			installed[id] = true
		}
	}
	ids := make([]string, 0, len(installed))
	for id := range installed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	visited := make(map[string]bool, len(ids))
	ordered := make([]string, 0, len(ids))
	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, dependency := range c.Apps[id].DependsOn {
			providerID, _, err := c.ResolveDependency(dependency)
			if err == nil && installed[providerID] {
				visit(providerID)
			}
		}
		ordered = append(ordered, id)
	}
	for _, id := range ids {
		visit(id)
	}
	return ordered
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	versionSeparator := false
	for index, char := range value {
		if char == '@' {
			if index == 0 || index == len(value)-1 || versionSeparator {
				return false
			}
			versionSeparator = true
			continue
		}
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func validRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
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
