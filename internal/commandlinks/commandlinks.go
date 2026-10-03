// Package commandlinks exposes selected installed commands without replacing
// existing files or commands owned by another installation.
package commandlinks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Link checks the entire command set before creating links. Repeated calls are
// safe. A collision leaves the app installed, but does not partially expose it.
func Link(home, installPath string, binaries []string) error {
	if len(binaries) == 0 {
		return nil
	}
	if err := prepare(home, installPath, true); err != nil {
		return err
	}
	targets := make(map[string]string)
	var pending []string
	for _, binary := range binaries {
		if !safePath(binary) {
			return fmt.Errorf("unsafe binary path %q", binary)
		}
		name := filepath.Base(binary)
		if _, exists := targets[name]; exists {
			return fmt.Errorf("duplicate command %q", name)
		}
		target := filepath.Join(home, filepath.FromSlash(installPath), filepath.FromSlash(binary))
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return fmt.Errorf("inspect command %s: %w", name, err)
		}
		root, err := filepath.EvalSymlinks(filepath.Join(home, filepath.FromSlash(installPath)))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("command %s resolves outside its installation", name)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("command %s is not an executable file", name)
		}
		link := filepath.Join(home, "bin", name)
		targets[name] = filepath.Join("..", filepath.FromSlash(installPath), filepath.FromSlash(binary))
		if _, err := os.Lstat(link); os.IsNotExist(err) {
			pending = append(pending, name)
		} else if err != nil {
			return err
		} else if !owned(link, targets[name]) {
			existing, err := os.Readlink(link)
			if err != nil {
				existing = "an existing file"
			}
			return fmt.Errorf("command collision at %s: occupied by %s; existing entry preserved", link, existing)
		}
	}
	var created []string
	for _, name := range pending {
		link := filepath.Join(home, "bin", name)
		if err := os.Symlink(targets[name], link); err != nil {
			for _, previous := range created {
				path := filepath.Join(home, "bin", previous)
				if owned(path, targets[previous]) {
					_ = os.Remove(path)
				}
			}
			return fmt.Errorf("link command %s: %w", name, err)
		}
		created = append(created, name)
	}
	return nil
}

// Remove only unlinks entries whose target matches this installation. It leaves
// regular files and links belonging to other installations untouched.
func Remove(home, installPath string, binaries []string) error {
	if len(binaries) == 0 {
		return nil
	}
	if err := prepare(home, installPath, false); err != nil {
		return err
	}
	for _, binary := range binaries {
		if !safePath(binary) {
			return fmt.Errorf("unsafe binary path %q", binary)
		}
	}
	for _, binary := range binaries {
		link := filepath.Join(home, "bin", filepath.Base(binary))
		target := filepath.Join("..", filepath.FromSlash(installPath), filepath.FromSlash(binary))
		if owned(link, target) {
			if err := os.Remove(link); err != nil {
				return fmt.Errorf("unlink %s: %w", link, err)
			}
		}
	}
	return nil
}

func owned(link, target string) bool {
	actual, err := os.Readlink(link)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(actual) {
		actual = filepath.Join(filepath.Dir(link), actual)
	}
	expected := filepath.Join(filepath.Dir(link), target)
	return filepath.Clean(actual) == filepath.Clean(expected)
}

func prepare(home, installPath string, create bool) error {
	if !safePath(installPath) || !strings.HasPrefix(installPath, "apps/") {
		return fmt.Errorf("unsafe installation path %q", installPath)
	}
	info, err := os.Lstat(home)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("YAPP home must be a real directory")
	}
	if create {
		current := home
		for _, part := range strings.Split(installPath, "/") {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("installation ancestor must be a real directory: %s", current)
			}
		}
	}
	bin := filepath.Join(home, "bin")
	if create {
		if err := os.Mkdir(bin, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err = os.Lstat(bin)
	if !create && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("YAPP bin must be a real directory: %s", bin)
	}
	return nil
}

func safePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
