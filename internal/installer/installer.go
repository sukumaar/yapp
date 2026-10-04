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
	"syscall"
	"time"

	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/progressui"
	"github.com/sukumaar/yapp/internal/state"
	"github.com/sukumaar/yapp/internal/validation"
)

const (
	maxDownloadSize = 512 << 20
	maxExtractSize  = 2 << 30
	maxTarEntries   = 100_000
	cacheMaxAge     = 30 * 24 * time.Hour
)

// Install downloads and installs one catalog app under the YAPP home directory.
// It never executes code from the downloaded archive.
func Install(ctx context.Context, home, appID string, app catalog.App, artifact catalog.Artifact, report progressui.Reporter) (state.Install, string, error) {
	report("Preparing install directories", 0, 0)
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
	archive, checksum, err := download(ctx, cacheDir, appID, app.Version, artifact, report)
	if err != nil {
		return state.Install{}, "", err
	}
	defer func() {
		_ = syscall.Flock(int(archive.Fd()), syscall.LOCK_UN)
		_ = archive.Close()
	}()

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

	report("Extracting verified archive", 0, 0)
	if err := extractTarGzip(archive, stage, artifact.StripComponents, report); err != nil {
		return state.Install{}, "", fmt.Errorf("extract %s: %w", app.Name, err)
	}
	report("Verifying installed executables", 0, 0)
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
	report("Publishing installation", 0, 0)
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
func Uninstall(home string, app catalog.App, install state.Install) error {
	if err := ensureExistingRealDir(home); err != nil {
		return err
	}
	installRoot := path.Dir(filepath.ToSlash(app.InstallPath))
	recordedPath := filepath.ToSlash(install.Path)
	if !validation.SafeRelativePath(app.InstallPath) || !validation.SafeRelativePath(install.Path) ||
		installRoot != "apps" && !strings.HasPrefix(installRoot, "apps/") || path.Dir(recordedPath) != installRoot {
		return fmt.Errorf("refusing to uninstall unsafe recorded path %q", install.Path)
	}
	components := strings.Split(recordedPath, "/")
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

// CleanupCache removes named package archives cached for more than 30 days.
// Archives currently used by an install are left in place.
func CleanupCache(ctx context.Context, home string, report func(string)) (int, error) {
	cacheDir := filepath.Join(home, "cache")
	cacheInfo, err := os.Lstat(cacheDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect package cache: %w", err)
	}
	if !cacheInfo.IsDir() || cacheInfo.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("package cache must be a real directory: %s", cacheDir)
	}
	entries, err := os.ReadDir(cacheDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read package cache: %w", err)
	}
	removed := 0
	cutoff := time.Now().Add(-cacheMaxAge)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		name := entry.Name()
		parts := strings.SplitN(strings.TrimSuffix(name, ".tar.gz"), "--", 3)
		if !strings.HasSuffix(name, ".tar.gz") || len(parts) != 3 || len(parts[0]) != 64 || parts[1] == "" || parts[2] == "" {
			continue
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			continue
		}
		archivePath := filepath.Join(cacheDir, name)
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		file, err := os.Open(archivePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("open stale cached archive: %w", err)
		}
		openedInfo, err := file.Stat()
		if err != nil || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			continue
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			if report != nil {
				report("Skipped cached archive currently in use: " + name)
			}
			continue
		}
		if err != nil {
			_ = file.Close()
			return removed, fmt.Errorf("lock stale cached archive: %w", err)
		}
		currentInfo, statErr := file.Stat()
		if statErr == nil && os.SameFile(info, currentInfo) && !currentInfo.ModTime().After(cutoff) {
			if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
				return removed, fmt.Errorf("remove stale cached archive: %w", err)
			}
			removed++
			if report != nil {
				report("Removed expired cached archive " + name)
			}
		}
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
	return removed, nil
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

func download(ctx context.Context, cacheDir, appID, version string, artifact catalog.Artifact, report progressui.Reporter) (*os.File, string, error) {
	arch := artifact.Arch
	if arch == "amd64" {
		arch = "x86_64"
	}
	name := fmt.Sprintf("%s--%s--%s.%s_%s.%s", strings.ToLower(artifact.SHA256), appID, version, arch, artifact.OS, artifact.Format)
	if filepath.Base(name) != name || !validation.SafeRelativePath(strings.ReplaceAll(name, "@", "-")) {
		return nil, "", fmt.Errorf("unsafe cache filename in catalog: %q", name)
	}
	cachedPath := filepath.Join(cacheDir, name)
	if archive, cached, err := openCachedArchive(ctx, cachedPath, artifact.SHA256); err != nil {
		return nil, "", err
	} else if cached {
		report("Using verified cached archive", 0, 0)
		return archive, strings.ToLower(artifact.SHA256), nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create download request: %w", err)
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
		return nil, "", fmt.Errorf("download artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download artifact: unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > maxDownloadSize {
		return nil, "", fmt.Errorf("artifact exceeds maximum download size")
	}

	file, err := os.CreateTemp(cacheDir, ".yapp-download-*")
	if err != nil {
		return nil, "", fmt.Errorf("create download file: %w", err)
	}
	filePath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return nil, "", fmt.Errorf("secure downloaded artifact permissions: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(filePath)
	}()

	hasher := sha256.New()
	report("Downloading artifact", 0, 0)
	progress := &downloadProgress{report: report, total: response.ContentLength}
	bytesWritten, err := io.Copy(io.MultiWriter(file, hasher, progress), io.LimitReader(response.Body, maxDownloadSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("save artifact: %w", err)
	}
	if bytesWritten > maxDownloadSize {
		return nil, "", fmt.Errorf("artifact exceeds maximum download size")
	}
	report("Verifying SHA-256 checksum", 0, 0)
	if err := file.Sync(); err != nil {
		return nil, "", fmt.Errorf("sync downloaded artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, "", fmt.Errorf("close downloaded artifact: %w", err)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actual, artifact.SHA256) {
		return nil, "", fmt.Errorf("artifact SHA-256 mismatch: expected %s, got %s", artifact.SHA256, actual)
	}
	if err := os.Rename(filePath, cachedPath); err != nil {
		return nil, "", fmt.Errorf("save verified archive to cache: %w", err)
	}
	if err := syncDirectory(cacheDir); err != nil {
		return nil, "", fmt.Errorf("sync archive cache: %w", err)
	}
	archive, cached, err := openCachedArchive(ctx, cachedPath, artifact.SHA256)
	if err != nil {
		return nil, "", err
	}
	if !cached {
		return nil, "", fmt.Errorf("verified archive disappeared from cache")
	}
	report("Saved verified archive for future installs", 0, 0)
	return archive, actual, nil
}

func openCachedArchive(ctx context.Context, path, expectedSHA256 string) (*os.File, bool, error) {
	for {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if !info.Mode().IsRegular() {
			return nil, false, fmt.Errorf("cached archive is not a regular file: %s", path)
		}

		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, false, err
		}
		openedInfo, err := file.Stat()
		if err != nil || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			if err != nil {
				return nil, false, err
			}
			continue
		}

		err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(25 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			_ = file.Close()
			return nil, false, err
		}

		valid, err := validCachedArchive(file, expectedSHA256)
		if err == nil && valid {
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
				return nil, false, err
			}
			return file, true, nil
		}
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		if err != nil {
			return nil, false, err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, false, fmt.Errorf("remove invalid cached archive: %w", err)
		}
		return nil, false, nil
	}
}

func validCachedArchive(file *os.File, expectedSHA256 string) (bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() > maxDownloadSize {
		return false, nil
	}
	hasher := sha256.New()
	read, err := io.Copy(hasher, io.LimitReader(file, maxDownloadSize+1))
	if err != nil {
		return false, err
	}
	if read > maxDownloadSize {
		return false, nil
	}
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedSHA256), nil
}

func extractTarGzip(archive io.Reader, destination string, stripComponents int, report progressui.Reporter) error {
	gzipReader, err := gzip.NewReader(archive)
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
		if entries%1000 == 0 {
			report(fmt.Sprintf("Extracted %d archive entries", entries), 0, 0)
		}
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
	report(fmt.Sprintf("Extracted %d archive entries", entries), 0, 0)
	return nil
}

type downloadProgress struct {
	report  progressui.Reporter
	total   int64
	written int64
	last    int64
}

func (p *downloadProgress) Write(data []byte) (int, error) {
	p.written += int64(len(data))
	if p.total <= 0 {
		if p.written-p.last >= 8<<20 {
			p.last = p.written
			p.report(fmt.Sprintf("Downloaded %d MiB", p.written>>20), 0, 0)
		}
		return len(data), nil
	}
	step := p.total / 100
	if step < 512<<10 {
		step = 512 << 10
	}
	if p.written-p.last >= step || p.written == p.total {
		p.last = p.written
		p.report("Downloading artifact", p.written, p.total)
	}
	return len(data), nil
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
