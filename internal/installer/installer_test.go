package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	oldName := strings.Repeat("a", 64) + "--jdk@25--25.0.4.1+1.x86_64_linux.tar.gz"
	oldPath := filepath.Join(cache, oldName)
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(cache, strings.Repeat("b", 64)+"--go@1--1.27.1.arm64_darwin.tar.gz")
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

func TestDownloadNamesAndReusesCache(t *testing.T) {
	const content = "downloaded archive"
	checksum := sha256.Sum256([]byte(content))
	wantSHA := hex.EncodeToString(checksum[:])
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, content)
	}))
	defer server.Close()
	cache := t.TempDir()
	for _, tc := range []struct {
		appID, version, goos, goarch, suffix string
	}{
		{"jdk@25", "25.0.4.1+1", "linux", "amd64", "--jdk@25--25.0.4.1+1.x86_64_linux.tar.gz"},
		{"jdk@25", "25.0.4.1+1", "linux", "arm64", "--jdk@25--25.0.4.1+1.arm64_linux.tar.gz"},
		{"jdk@25", "25.0.4.1+1", "darwin", "arm64", "--jdk@25--25.0.4.1+1.arm64_darwin.tar.gz"},
		{"go@1", "1.2.3", "linux", "amd64", "--go@1--1.2.3.x86_64_linux.tar.gz"},
		{"go@1", "1.2.4", "linux", "amd64", "--go@1--1.2.4.x86_64_linux.tar.gz"},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			artifact := catalog.Artifact{SHA256: wantSHA, URL: server.URL, Arch: tc.goarch, OS: tc.goos, Format: "tar.gz"}
			before := requests.Load()
			for range 2 {
				archive, gotSHA, err := download(context.Background(), cache, tc.appID, tc.version, artifact, func(string, int64, int64) {})
				if err != nil {
					t.Fatal(err)
				}
				gotName := filepath.Base(archive.Name())
				data, readErr := io.ReadAll(archive)
				_ = archive.Close()
				if readErr != nil || string(data) != content || gotSHA != wantSHA || gotName != wantSHA+tc.suffix {
					t.Fatalf("unexpected cached download: name=%q checksum=%q data=%q error=%v", gotName, gotSHA, data, readErr)
				}
			}
			if got := requests.Load() - before; got != 1 {
				t.Fatalf("downloaded %d times, want one download followed by cache reuse", got)
			}
		})
	}
	artifact := catalog.Artifact{SHA256: wantSHA, URL: server.URL, Arch: "amd64", OS: "../../outside", Format: "tar.gz"}
	if _, _, err := download(context.Background(), cache, "jdk@25", "25.0.4.1+1", artifact, func(string, int64, int64) {}); err == nil {
		t.Fatal("accepted unsafe metadata in cache filename")
	}
}

func TestOpenCachedArchiveDeletesChecksumMismatch(t *testing.T) {
	cache := t.TempDir()
	archivePath := filepath.Join(cache, strings.Repeat("a", 64)+"--go@1--1.27.1.arm64_darwin.tar.gz")
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
