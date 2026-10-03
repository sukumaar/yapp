package catalog

import "testing"

func TestVersionedIdentifiers(t *testing.T) {
	for _, id := range []string{"jdk@25", "node@24", "maven@3", "python-standalone@3", "sbt@2", "go@1", "rust@1", "scala@3"} {
		if !validIdentifier(id) {
			t.Errorf("validIdentifier(%q) = false", id)
		}
	}
	for _, id := range []string{"@jdk", "jdk@", "jdk@@25", "jdk@25!"} {
		if validIdentifier(id) {
			t.Errorf("validIdentifier(%q) = true", id)
		}
	}
}

func TestCatalogAliases(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for alias, wantID := range map[string]string{"go": "go@1", "rust": "rust@1"} {
		id, app, ok := c.ResolveApp(alias)
		if !ok || id != wantID || app.Name == "" {
			t.Errorf("ResolveApp(%q) = (%q, %q, %v); want %q", alias, id, app.Name, ok, wantID)
		}
	}
	for _, alias := range []string{"go", "rust"} {
		c.Aliases[alias] = "missing@1"
		if err := c.Validate(); err == nil {
			t.Errorf("Validate accepted alias %q with unknown target", alias)
		}
		c, err = Default()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalogPlatforms(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for id, app := range c.Apps {
		for _, platform := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}} {
			if _, err := app.ArtifactFor(platform[0], platform[1]); err != nil {
				t.Errorf("app %q: %v", id, err)
			}
		}
	}
}

func TestNodeIncludesNPM(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	node := c.Apps["node@24"]
	if node.Version != "24.21.0" || node.SemanticVersion != "24.21.0" {
		t.Fatalf("unexpected Node.js version: %s (%s)", node.Version, node.SemanticVersion)
	}
	for _, command := range []string{"bin/node", "bin/npm", "bin/npx"} {
		found := false
		for _, linked := range node.LinkedBinaries() {
			found = found || linked == command
		}
		if !found {
			t.Errorf("Node.js does not expose %s", command)
		}
	}
}

func TestInstallModeValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config InstallMode
		valid  bool
	}{
		{"enabled", InstallMode{Symlink: true, Binaries: []string{"bin/java", "bin/javac"}}, true},
		{"disabled", InstallMode{Symlink: false}, true},
		{"empty", InstallMode{Symlink: true}, false},
		{"traversal", InstallMode{Symlink: true, Binaries: []string{"../java"}}, false},
		{"duplicate name", InstallMode{Symlink: true, Binaries: []string{"bin/java", "other/java"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Default()
			if err != nil {
				t.Fatal(err)
			}
			app := c.Apps["jdk@25"]
			app.InstallMode = &tt.config
			c.Apps["jdk@25"] = app
			if err := c.Validate(); (err == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, err)
			}
		})
	}
}

func TestDependencyConstraints(t *testing.T) {
	for _, tt := range []struct {
		constraint string
		valid      bool
	}{
		{">=17", true}, {"25.0.4", true}, {"^25.0.0", true},
		{"<17", false}, {"25.0.3", false}, {"bad", false},
	} {
		t.Run(tt.constraint, func(t *testing.T) {
			c, err := Default()
			if err != nil {
				t.Fatal(err)
			}
			app := c.Apps["maven@3"]
			app.DependsOn[0].Version = tt.constraint
			c.Apps["maven@3"] = app
			if err := c.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate = %v; want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestDependencyNameResolvesVersionedProvider(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	id, app, err := c.ResolveDependency(Dependency{Name: "jdk", Version: "25.x"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "jdk@25" || app.Version != "25.0.4.1+1" {
		t.Fatalf("resolved %q at %q; want jdk@25", id, app.Version)
	}
}

func TestNonSemVerReleaseNeedsExplicitMapping(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	app := c.Apps["jdk@25"]
	app.SemanticVersion = ""
	c.Apps["jdk@25"] = app
	if err := c.Validate(); err == nil {
		t.Fatal("accepted unmapped four-part release")
	}
}
