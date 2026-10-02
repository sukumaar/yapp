// Package catalog loads and validates YAPP's embedded app catalog.
package catalog

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var files embed.FS

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// Catalog contains installable apps keyed by their CLI identifier.
type Catalog struct {
	Apps map[string]App `yaml:"apps"`
}

// App describes one pinned version of an app.
type App struct {
	Name        string     `yaml:"name"`
	Version     string     `yaml:"version"`
	ReleaseURL  string     `yaml:"release_url"`
	Artifacts   []Artifact `yaml:"artifacts"`
	InstallPath string     `yaml:"install_path"`
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
	data, err := files.ReadFile("catalog.yaml")
	if err != nil {
		return Catalog{}, fmt.Errorf("read embedded catalog: %w", err)
	}
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
		if app.ReleaseURL != "" {
			releaseURL, err := url.ParseRequestURI(app.ReleaseURL)
			if err != nil || releaseURL.Scheme != "https" || releaseURL.Host == "" || releaseURL.User != nil {
				return fmt.Errorf("app %q has an invalid or non-HTTPS release URL", id)
			}
		}
		if !validRelativePath(app.InstallPath) {
			return fmt.Errorf("app %q has an unsafe install_path", id)
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

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
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
