package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/state"
)

func TestExistingInstallLinksAndUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	c, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	app := c.Apps["jdk25"]
	yapp := filepath.Join(home, ".yapp")
	for _, binary := range app.LinkedBinaries() {
		path := filepath.Join(yapp, app.InstallPath, binary)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	a := app.Artifacts[0]
	record := state.Install{Name: app.Name, Version: app.Version, Path: app.InstallPath, ArtifactURL: a.URL, SHA256: a.SHA256, OS: a.OS, Arch: a.Arch}
	if err := state.Record(yapp, "jdk25", record); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for i := 0; i < 2; i++ {
		if err := Execute(context.Background(), []string{"install", "jdk25"}, &output); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"java", "javac", "javap", "jar", "jshell"} {
		if _, err := os.Readlink(filepath.Join(yapp, "bin", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(filepath.Join(yapp, "bin/javadoc")); !os.IsNotExist(err) {
		t.Fatal("exposed unlisted command")
	}
	env, err := os.ReadFile(filepath.Join(yapp, "yapp-env.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "${YAPP_HOME}/bin") || strings.Contains(string(env), app.InstallPath+"/bin") {
		t.Fatalf("unexpected PATH configuration: %s", env)
	}
	if err := Execute(context.Background(), []string{"uninstall", "jdk25"}, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(yapp, "bin/java")); !os.IsNotExist(err) {
		t.Fatal("owned link remains")
	}
	loaded, err := state.Load(yapp)
	if err != nil || len(loaded.Apps) != 0 {
		t.Fatalf("state=%v err=%v", loaded, err)
	}
}

func TestSymlinkFalseDoesNotExposeAppPath(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".yapp"), 0o700); err != nil {
		t.Fatal(err)
	}
	c := catalog.Catalog{Apps: map[string]catalog.App{"tool": {InstallMode: &catalog.InstallMode{Symlink: false, Binaries: []string{"bin/tool"}}, Environment: catalog.EnvironmentConfig{Paths: []string{"bin"}}}}}
	s := state.State{Apps: map[string]state.Install{"tool": {Path: "apps/tool/1"}}}
	if err := configureShell(home, "bash", c, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".yapp/yapp-env.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "apps/tool") {
		t.Fatal("exposed isolated app through PATH")
	}
}
