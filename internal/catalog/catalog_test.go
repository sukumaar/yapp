package catalog

import "testing"

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
			app := c.Apps["jdk25"]
			app.InstallMode = &tt.config
			c.Apps["jdk25"] = app
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
			app := c.Apps["maven"]
			app.DependsOn[0].Version = tt.constraint
			c.Apps["maven"] = app
			if err := c.Validate(); (err == nil) != tt.valid {
				t.Fatalf("Validate = %v; want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestNonSemVerReleaseNeedsExplicitMapping(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	app := c.Apps["jdk25"]
	app.SemanticVersion = ""
	c.Apps["jdk25"] = app
	if err := c.Validate(); err == nil {
		t.Fatal("accepted unmapped four-part release")
	}
}
