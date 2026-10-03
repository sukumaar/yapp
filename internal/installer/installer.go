// Package installer downloads, verifies, and extracts catalog artifacts.
package installer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/state"
	"github.com/sukumaar/yapp/internal/validation"
)

const (
	maxDownloadSize = 512 << 20
	maxExtractSize  = 2 << 30
	maxTarEntries   = 100_000
)

// Install downloads and installs one catalog app under the YAPP home directory.
// It never executes code from the downloaded archive.
func Install(ctx context.Context, home string, app catalog.App, artifact catalog.Artifact) (state.Install, string, error) {
	if err := ensureRealDir(home, 0o700); err != nil {
		return state.Install{}, "", err
	}
	if !validation.SafeRelativePath(app.InstallPath) {
		return state.Install{}, "", fmt.Errorf("unsafe install path in catalog")
	}
	installPath := filepath.Join(home, filepath.FromSlash(app.InstallPath))
	parent := filepath.Dir(installPath)
	if err := ensurePathDirs(home, filepath.Dir(filepath.FromSlash(app.InstallPath))); err != nil {
		return state.Install{}, "", err
	}
	if _, err := os.Lstat(installPath); err == nil {
		return state.Install{}, "", fmt.Errorf("%s %s is already present at %s", app.Name, app.Version, installPath)
	} else if !os.IsNotExist(err) {
		return state.Install{}, "", fmt.Errorf("inspect install destination: %w", err)
	}

	cacheDir := filepath.Join(home, "cache")
	if err := ensureRealDir(cacheDir, 0o700); err != nil {
		return state.Install{}, "", err
	}
	archivePath, checksum, err := download(ctx, cacheDir, artifact)
	if err != nil {
		return state.Install{}, "", err
	}
	defer os.Remove(archivePath)

	stage, err := os.MkdirTemp(parent, ".yapp-install-*")
	if err != nil {
		return state.Install{}, "", fmt.Errorf("create staging directory: %w", err)
	}
	_ = os.Chmod(stage, 0o700)
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
		}
	}()

	if err := extractTarGzip(archivePath, stage, artifact.StripComponents); err != nil {
		return state.Install{}, "", fmt.Errorf("extract %s: %w", app.Name, err)
	}
	for _, executable := range app.Executables {
		if err := verifyExecutable(filepath.Join(stage, filepath.FromSlash(executable)), executable); err != nil {
			return state.Install{}, "", err
		}
	}
	for _, executable := range app.LinkedBinaries() {
		if err := verifyExecutable(filepath.Join(stage, filepath.FromSlash(executable)), executable); err != nil {
			return state.Install{}, "", err
		}
	}
	if err := os.Rename(stage, installPath); err != nil {
		return state.Install{}, "", fmt.Errorf("publish installation: %w", err)
	}
	committed = true
	if err := syncDirectory(parent); err != nil {
		return state.Install{}, "", fmt.Errorf("sync install directory: %w", err)
	}

	record := state.Install{
		Name:            app.Name,
		Version:         app.Version,
		SemanticVersion: app.SemanticVersion,
		Path:            filepath.ToSlash(app.InstallPath),
		ArtifactURL:     artifact.URL,
		SHA256:          checksum,
		OS:              artifact.OS,
		Arch:            artifact.Arch,
		InstalledAt:     time.Now().UTC(),
		LinkedBinaries:  app.LinkedBinaries(),
	}
	return record, installPath, nil
}

// Uninstall removes only a recorded app directory beneath the YAPP apps tree.
func Uninstall(home, appID string, install state.Install) error {
	if err := ensureExistingRealDir(home); err != nil {
		return err
	}
	if !validation.SafeRelativePath(appID) || !validation.SafeRelativePath(install.Path) || !strings.HasPrefix(filepath.ToSlash(install.Path), "apps/"+filepath.ToSlash(appID)+"/") {
		return fmt.Errorf("refusing to uninstall unsafe recorded path %q", install.Path)
	}
	components := strings.Split(filepath.ToSlash(install.Path), "/")
	current := home
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect recorded install path %s: %w", current, err)
		}
		if index < len(components)-1 {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to uninstall through a non-directory or symlink: %s", current)
			}
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to uninstall a non-directory or symlink: %s", current)
		}
	}
	parent := filepath.Dir(current)
	if err := os.RemoveAll(current); err != nil {
		return fmt.Errorf("remove app directory %s: %w", current, err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync app directory after uninstall: %w", err)
	}
	return nil
}

func ensureExistingRealDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect YAPP home: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("YAPP home must be a real directory: %s", path)
	}
	return nil
}

func download(ctx context.Context, cacheDir string, artifact catalog.Artifact) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create download request: %w", err)
	}
	request.Header.Set("User-Agent", "yapp/"+"dev")
	client := &http.Client{
		Timeout: 20 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many download redirects")
			}
			if request.URL.Scheme != "https" {
				return fmt.Errorf("refusing non-HTTPS download redirect")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("download artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download artifact: unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > maxDownloadSize {
		return "", "", fmt.Errorf("artifact exceeds maximum download size")
	}

	file, err := os.CreateTemp(cacheDir, ".yapp-download-*")
	if err != nil {
		return "", "", fmt.Errorf("create download file: %w", err)
	}
	filePath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return "", "", fmt.Errorf("secure downloaded artifact permissions: %w", err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(filePath)
		}
	}()

	hasher := sha256.New()
	bytesWritten, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(response.Body, maxDownloadSize+1))
	if err != nil {
		return "", "", fmt.Errorf("save artifact: %w", err)
	}
	if bytesWritten > maxDownloadSize {
		return "", "", fmt.Errorf("artifact exceeds maximum download size")
	}
	if err := file.Sync(); err != nil {
		return "", "", fmt.Errorf("sync downloaded artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", "", fmt.Errorf("close downloaded artifact: %w", err)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actual, artifact.SHA256) {
		return "", "", fmt.Errorf("artifact SHA-256 mismatch: expected %s, got %s", artifact.SHA256, actual)
	}
	keep = true
	return filePath, actual, nil
}

func extractTarGzip(archivePath, destination string, stripComponents int) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)

	var totalSize int64
	entries := 0
	directoryModes := make(map[string]os.FileMode)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		entries++
		if entries > maxTarEntries {
			return fmt.Errorf("archive contains too many entries")
		}
		relative, include, err := safeArchivePath(header.Name, stripComponents)
		if err != nil {
			return err
		}
		if !include {
			continue
		}
		fullPath := filepath.Join(destination, filepath.FromSlash(relative))
		if err := ensureArchiveParents(destination, relative); err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureArchiveDir(destination, relative); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode).Perm() & 0o755
			if mode == 0 {
				mode = 0o755
			}
			directoryModes[fullPath] = mode
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxExtractSize-totalSize {
				return fmt.Errorf("archive expands beyond maximum extracted size")
			}
			totalSize += header.Size
			mode := 0o644 | (os.FileMode(header.Mode).Perm() & 0o111)
			out, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create archive file %q: %w", relative, err)
			}
			copied, copyErr := io.CopyN(out, reader, header.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("write archive file %q: %w", relative, copyErr)
			}
			if copied != header.Size {
				return fmt.Errorf("truncated archive file %q", relative)
			}
			if closeErr != nil {
				return fmt.Errorf("close archive file %q: %w", relative, closeErr)
			}
		case tar.TypeSymlink:
			if err := createSafeSymlink(destination, relative, header.Linkname); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d for %q", header.Typeflag, relative)
		}
	}
	if _, err := io.Copy(io.Discard, gzipReader); err != nil {
		return fmt.Errorf("verify gzip stream: %w", err)
	}

	for dir, mode := range directoryModes {
		if err := os.Chmod(dir, mode); err != nil {
			return fmt.Errorf("set directory permissions: %w", err)
		}
	}
	return nil
}

func safeArchivePath(name string, stripComponents int) (string, bool, error) {
	if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("unsafe archive path %q", name)
	}
	raw := strings.TrimSuffix(name, "/")
	for _, component := range strings.Split(raw, "/") {
		if component == ".." {
			return "", false, fmt.Errorf("archive path contains traversal: %q", name)
		}
	}
	clean := path.Clean(raw)
	if clean == "." {
		return "", false, nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, fmt.Errorf("archive path escapes installation directory: %q", name)
	}
	components := strings.Split(clean, "/")
	for _, component := range components {
		if component == ".." {
			return "", false, fmt.Errorf("archive path contains traversal: %q", name)
		}
	}
	if len(components) <= stripComponents {
		return "", false, nil
	}
	relative := strings.Join(components[stripComponents:], "/")
	if relative == "" || relative == "." {
		return "", false, nil
	}
	return relative, true, nil
}

func ensureArchiveParents(root, relative string) error {
	components := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	current := root
	for _, component := range components[:len(components)-1] {
		current = filepath.Join(current, component)
		if err := ensureArchiveDir(root, filepath.ToSlash(strings.TrimPrefix(current, root+string(filepath.Separator)))); err != nil {
			return err
		}
	}
	return nil
}

func ensureArchiveDir(root, relative string) error {
	fullPath := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(fullPath)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive path conflicts with a non-directory: %q", relative)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect archive directory %q: %w", relative, err)
	}
	if err := os.Mkdir(fullPath, 0o700); err != nil {
		return fmt.Errorf("create archive directory %q: %w", relative, err)
	}
	return nil
}

func createSafeSymlink(root, relative, target string) error {
	if target == "" || strings.Contains(target, `\`) || path.IsAbs(target) {
		return fmt.Errorf("unsafe symlink target %q", target)
	}
	resolved := path.Clean(path.Join(path.Dir(relative), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") || path.IsAbs(resolved) {
		return fmt.Errorf("symlink escapes installation directory: %q", relative)
	}
	linkPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.Symlink(filepath.FromSlash(target), linkPath); err != nil {
		return fmt.Errorf("create archive symlink %q: %w", relative, err)
	}
	return nil
}

func ensureRealDir(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("expected a real directory at %s", path)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("secure directory permissions: %w", err)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect directory %s: %w", path, err)
	}
	if err := os.Mkdir(path, mode); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return ensureRealDir(path, mode)
}

func ensurePathDirs(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		if err := ensureRealDir(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func verifyExecutable(path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("archive does not contain %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("archive %s is not an executable regular file", name)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
