// Package shellenv maintains YAPP's per-user shell PATH configuration.
package shellenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sukumaar/yapp/internal/fileutil"
	"github.com/sukumaar/yapp/internal/validation"
)

const (
	beginMarker = "# >>> yapp managed >>>"
	endMarker   = "# <<< yapp managed <<<"
	maxRCSize   = 1 << 20
)

// Detect returns the user's configured interactive shell name.
func Detect() (string, error) {
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "bash", "zsh":
		return shell, nil
	case "":
		return "", fmt.Errorf("SHELL is not set; set it to bash or zsh before installing")
	default:
		return "", fmt.Errorf("unsupported shell %q; YAPP currently supports bash and zsh", shell)
	}
}

// Configure writes the current app environment and sources it from the shell startup file.
func Configure(userHome, shell string, variables map[string]string, pathEntries []string, report func(string)) error {
	if shell != "bash" && shell != "zsh" {
		return fmt.Errorf("unsupported shell %q", shell)
	}

	yappHome := filepath.Join(userHome, ".yapp")
	info, err := os.Lstat(yappHome)
	if err != nil {
		return fmt.Errorf("inspect YAPP home: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("YAPP home must be a real directory: %s", yappHome)
	}
	var environment bytes.Buffer
	if err := Render(&environment, variables, pathEntries); err != nil {
		return err
	}
	if err := writeEnvironment(yappHome, environment.Bytes(), report); err != nil {
		return fmt.Errorf("write YAPP shell environment: %w", err)
	}
	if err := setStartupBlock(userHome, startupFile(shell, runtime.GOOS)); err != nil {
		return err
	}
	return nil
}

func startupFile(shell, goos string) string {
	switch {
	case shell == "bash" && goos == "darwin":
		return ".bash_profile"
	case shell == "zsh" && goos == "darwin":
		return ".zprofile"
	case shell == "bash":
		return ".bashrc"
	default:
		return ".zshrc"
	}
}

func writeEnvironment(yappHome string, data []byte, report func(string)) error {
	lockPath := filepath.Join(yappHome, ".yapp-env.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	started := time.Now()
	waiting, hinted := false, false
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if !waiting {
			notify(report, "Waiting for another YAPP command to finish updating yapp-env.sh")
			waiting = true
		}
		if !hinted && time.Since(started) >= time.Minute {
			notify(report, fmt.Sprintf("Still waiting after 60 seconds. Lock file: %s. It normally remains after use. If all YAPP commands are stopped and you suspect it is stuck, you may remove it, then retry.", lockPath))
			hinted = true
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	path := filepath.Join(yappHome, "yapp-env.sh")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return fileutil.AtomicWrite(path, data, 0o600)
}

func notify(report func(string), message string) {
	if report != nil {
		report(message)
		return
	}
	fmt.Fprintln(os.Stderr, message)
}

func Render(w io.Writer, variables map[string]string, pathEntries []string) error {
	for name, value := range variables {
		if !validation.EnvironmentName(name) || !validation.SafeRelativePath(value) {
			return fmt.Errorf("unsafe YAPP environment variable setting %q", name)
		}
	}
	for _, path := range pathEntries {
		if !validation.SafeRelativePath(path) {
			return fmt.Errorf("unsafe YAPP PATH entry %q", path)
		}
	}
	if _, err := io.WriteString(w, "export YAPP_HOME=\"${HOME}/.yapp\"\n"); err != nil {
		return fmt.Errorf("write YAPP shell environment: %w", err)
	}
	for i := len(pathEntries) - 1; i >= 0; i-- {
		bin := "${YAPP_HOME}/" + filepath.ToSlash(pathEntries[i])
		if _, err := fmt.Fprintf(w, "case \":${PATH:-}:\" in\n  *\":%s:\"*) ;;\n  *) export PATH=\"%s${PATH:+:${PATH}}\" ;;\nesac\n", bin, bin); err != nil {
			return fmt.Errorf("write YAPP shell environment: %w", err)
		}
	}
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintf(w, "export %s=\"${YAPP_HOME}/%s\"\n", name, filepath.ToSlash(variables[name])); err != nil {
			return fmt.Errorf("write YAPP shell environment: %w", err)
		}
	}
	return nil
}

func setStartupBlock(userHome, rcName string) error {
	rcPath := filepath.Join(userHome, rcName)
	mode := os.FileMode(0o600)
	if info, err := os.Lstat(rcPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file", rcName)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", rcName, err)
	}
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", rcName, err)
	}
	if len(existing) > maxRCSize {
		return fmt.Errorf("%s exceeds the maximum supported size", rcName)
	}
	source := `. "${HOME}/.yapp/yapp-env.sh"`
	updated, err := replaceBlock(string(existing), beginMarker, endMarker, source)
	if err != nil {
		return fmt.Errorf("%s: %w", rcName, err)
	}
	updated = strings.ReplaceAll(updated, beginMarker, "")
	updated = strings.ReplaceAll(updated, endMarker, "")
	if !strings.Contains(updated, source) {
		updated = strings.TrimRight(updated, "\n") + "\n" + source + "\n"
	}
	if len(updated) > maxRCSize {
		return fmt.Errorf("adding YAPP integration would make %s too large", rcName)
	}
	if string(existing) == updated {
		return nil
	}
	return fileutil.AtomicWrite(rcPath, []byte(updated), mode)
}

func replaceBlock(data, begin, end, replacement string) (string, error) {
	start := strings.Index(data, begin)
	finish := strings.Index(data, end)
	if start < 0 && finish < 0 {
		return data, nil
	}
	if start < 0 || finish < start {
		return "", fmt.Errorf("contains an incomplete YAPP-managed block; repair it before installing")
	}
	finish += len(end)
	return data[:start] + replacement + data[finish:], nil
}
