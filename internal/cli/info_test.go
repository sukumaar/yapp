package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/state"
)

func TestWriteAppInfo(t *testing.T) {
	app := catalog.App{
		Name:        "Node.js",
		Version:     "24.21.0",
		InstallPath: "apps/node/24.21.0",
		ReleaseURL:  "https://nodejs.org/release",
		Executables: []string{"bin/node"},
		InstallMode: &catalog.InstallMode{Symlink: true, Binaries: []string{"bin/node", "bin/npm"}},
		Artifacts:   []catalog.Artifact{{OS: "linux", Arch: "amd64", Format: "tar.gz", URL: "https://example.test/node.tar.gz", SHA256: "abc123"}},
	}
	var output bytes.Buffer
	err := writeAppInfo(&output, "node24", app, state.Install{Version: app.Version, Path: app.InstallPath}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"==> node24: Node.js 24.21.0",
		"Installed:    yes (24.21.0) — ~/.yapp/apps/node/24.21.0",
		"Commands:     node, npm",
		"linux/amd64 (tar.gz)",
		"SHA-256: abc123",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("info output missing %q:\n%s", expected, output.String())
		}
	}
}
