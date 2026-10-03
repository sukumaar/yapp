package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/state"
)

func TestUninstallUsesCatalogInstallRoot(t *testing.T) {
	home := t.TempDir()
	installPath := filepath.Join(home, "apps/node/24.21.0")
	if err := os.MkdirAll(installPath, 0o700); err != nil {
		t.Fatal(err)
	}
	app := catalog.App{InstallPath: "apps/node/24.21.0"}
	install := state.Install{Path: "apps/node/24.21.0"}

	if err := Uninstall(home, app, install); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(installPath); !os.IsNotExist(err) {
		t.Fatalf("installation still exists, lstat error=%v", err)
	}
}

func TestUninstallRejectsDifferentCatalogInstallRoot(t *testing.T) {
	home := t.TempDir()
	app := catalog.App{InstallPath: "apps/node/24.21.0"}
	install := state.Install{Path: "apps/other/1.0.0"}

	if err := Uninstall(home, app, install); err == nil {
		t.Fatal("uninstall accepted a path outside the app's catalog root")
	}
}

func TestCleanupCacheRemovesOnlyExpiredArchives(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	if err := os.Mkdir(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	oldName := strings.Repeat("a", 64) + ".tar.gz"
	oldPath := filepath.Join(cache, oldName)
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(cache, strings.Repeat("b", 64)+".tar.gz")
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(cache, "keep-me")
	if err := os.WriteFile(otherPath, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := CleanupCache(context.Background(), home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d archives, want 1", removed)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expired archive remains, stat error=%v", err)
	}
	for _, path := range []string{newPath, otherPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected cache entry to remain at %s: %v", path, err)
		}
	}
}

func TestOpenCachedArchiveDeletesChecksumMismatch(t *testing.T) {
	cache := t.TempDir()
	archivePath := filepath.Join(cache, strings.Repeat("a", 64)+".tar.gz")
	if err := os.WriteFile(archivePath, []byte("corrupted archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("expected archive"))
	archive, cached, err := openCachedArchive(context.Background(), archivePath, hex.EncodeToString(want[:]))
	if err != nil {
		t.Fatal(err)
	}
	if cached || archive != nil {
		t.Fatal("checksum-mismatched archive was accepted")
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("checksum-mismatched archive was not removed, stat error=%v", err)
	}
}
