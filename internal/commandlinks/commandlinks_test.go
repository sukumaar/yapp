package commandlinks

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, app := range []string{"first", "second"} {
		dir := filepath.Join(home, "apps", app, "1", "bin")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"tool", "other"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	return home
}

func TestLinkOwnershipAndRetry(t *testing.T) {
	home := fixture(t)
	binaries := []string{"bin/tool", "bin/other"}
	for i := 0; i < 2; i++ {
		if err := Link(home, "apps/first/1", binaries); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(home, "bin/tool")
	if target, err := os.Readlink(link); err != nil || target != "../apps/first/1/bin/tool" {
		t.Fatalf("target=%q err=%v", target, err)
	}
	if err := Link(home, "apps/second/1", binaries); err == nil {
		t.Fatal("overwrote another owner")
	}
	if err := Remove(home, "apps/second/1", binaries); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(link); err != nil {
		t.Fatal("removed first owner's link", err)
	}
	// Cleanup must also work after the package directory has been deleted.
	if err := os.RemoveAll(filepath.Join(home, "apps/first/1")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(home, "apps/first/1", binaries); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link remains: %v", err)
	}
}

func TestCollisionLeavesWholeSetUnchanged(t *testing.T) {
	home := fixture(t)
	if err := os.Mkdir(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "bin/other")
	if err := os.WriteFile(other, []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Link(home, "apps/first/1", []string{"bin/tool", "bin/other"}); err == nil {
		t.Fatal("expected collision")
	}
	if _, err := os.Lstat(filepath.Join(home, "bin/tool")); !os.IsNotExist(err) {
		t.Fatal("partially linked command set")
	}
	if err := Remove(home, "apps/first/1", []string{"bin/other"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(other)
	if err != nil || string(data) != "user file" {
		t.Fatal("changed user file")
	}
}

func TestRejectUnsafeLinks(t *testing.T) {
	for _, binary := range []string{"../escape", "/absolute", "bin/missing"} {
		t.Run(binary, func(t *testing.T) {
			if err := Link(fixture(t), "apps/first/1", []string{binary}); err == nil {
				t.Fatal("accepted invalid binary")
			}
		})
	}
	t.Run("symlink bin", func(t *testing.T) {
		home := fixture(t)
		if err := os.Symlink(t.TempDir(), filepath.Join(home, "bin")); err != nil {
			t.Fatal(err)
		}
		if err := Link(home, "apps/first/1", []string{"bin/tool"}); err == nil {
			t.Fatal("followed symlink bin")
		}
		if err := Remove(home, "apps/first/1", []string{"bin/tool"}); err == nil {
			t.Fatal("followed symlink bin during removal")
		}
	})
	t.Run("escaping target", func(t *testing.T) {
		home := fixture(t)
		if err := os.Symlink(filepath.Join(home, "apps/second/1/bin/tool"), filepath.Join(home, "apps/first/1/bin/escape")); err != nil {
			t.Fatal(err)
		}
		if err := Link(home, "apps/first/1", []string{"bin/escape"}); err == nil {
			t.Fatal("linked outside installation")
		}
	})
}
