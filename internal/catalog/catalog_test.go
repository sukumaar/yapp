package catalog

import "testing"

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
