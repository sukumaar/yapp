package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/state"
)

func TestDependencyStatus(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "apps/runtime/old/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(home, "apps/runtime/old/bin/tool")
	if err := os.WriteFile(binary, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := catalog.App{Version: "25.0.4.1+1", SemanticVersion: "25.0.4", Executables: []string{"bin/tool"}, Artifacts: []catalog.Artifact{{URL: "url", SHA256: "hash"}}}
	requirement := catalog.Dependency{Name: "runtime", Version: ">=17"}
	for _, tt := range []struct{ release, semantic, want string }{
		{"17.0.0", "", ""},
		{"11.0.0", "", "does not satisfy"},
		{"25.0.4.1+1", "25.0.4", ""},
		{"25.0.4.1+1", "", ""}, // Legacy registry, identical catalog artifact.
		{"24.0.1.1+1", "", "unverified"},
	} {
		t.Run(tt.release+tt.semantic, func(t *testing.T) {
			installed := state.State{Apps: map[string]state.Install{"runtime": {Version: tt.release, SemanticVersion: tt.semantic, Path: "apps/runtime/old", ArtifactURL: "url", SHA256: "hash"}}}
			// A mismatched managed runtime on PATH must not override its registry version.
			t.Setenv("PATH", filepath.Dir(binary))
			got := dependencyStatus(home, "runtime", requirement, app, installed)
			if (tt.want == "" && got != "") || (tt.want != "" && !strings.Contains(got, tt.want)) {
				t.Fatalf("status=%q; want %q", got, tt.want)
			}
		})
	}
	t.Setenv("PATH", filepath.Dir(binary))
	if got := dependencyStatus(home, "runtime", requirement, app, state.State{}); !strings.Contains(got, "unverified") {
		t.Fatalf("external executable treated as compatible: %s", got)
	}
}

func TestEveryDependencyEdgeChecksItsConstraint(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "runtime/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "runtime/bin/tool"), []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	c := catalog.Catalog{Apps: map[string]catalog.App{
		"runtime": {Version: "21.0.0", Executables: []string{"bin/tool"}},
		"builder": {Version: "1.0.0", Executables: []string{"bin/builder"}, DependsOn: []catalog.Dependency{{Name: "runtime", Version: ">=21"}}},
	}}
	app := catalog.App{DependsOn: []catalog.Dependency{{Name: "runtime", Version: ">=17"}, {Name: "builder", Version: "1.0.0"}}}
	installed := state.State{Apps: map[string]state.Install{"runtime": {Version: "17.0.0", Path: "runtime"}}}
	var output bytes.Buffer
	if err := writeDependencyHints(&output, home, c, app, installed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "runtime (>=21)") {
		t.Fatalf("stricter edge was skipped: %s", output.String())
	}
}
