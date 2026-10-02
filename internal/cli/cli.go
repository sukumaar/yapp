// Package cli implements YAPP's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/sukumaar/yapp/internal/buildinfo"
	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/installer"
	"github.com/sukumaar/yapp/internal/shellenv"
	"github.com/sukumaar/yapp/internal/state"
	"github.com/sukumaar/yapp/internal/versionconstraint"
)

// ExitError reports an error together with the process exit code to use.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// ExitCode returns the process exit code associated with err.
func ExitCode(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}

// Execute handles implemented CLI commands. Package-management commands that
// are not yet available are reported explicitly.
func Execute(ctx context.Context, args []string, stdout io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("operation canceled: %w", err)
	}

	if len(args) == 0 {
		return writeHelp(stdout)
	}

	switch args[0] {
	case "install":
		return installApp(ctx, args[1:], stdout)
	case "uninstall":
		return uninstallApp(args[1:], stdout)
	case "help", "--help", "-h":
		if len(args) != 1 {
			return &ExitError{Code: 2, Message: "help does not accept arguments"}
		}
		return writeHelp(stdout)
	case "version", "--version", "-v":
		if len(args) != 1 {
			return &ExitError{Code: 2, Message: "version does not accept arguments"}
		}
		if _, err := fmt.Fprintln(stdout, buildinfo.String()); err != nil {
			return fmt.Errorf("write version: %w", err)
		}
		return nil
	case "list", "info", "search", "update", "outdated", "upgrade", "doctor", "cleanup":
		return &ExitError{
			Code:    1,
			Message: fmt.Sprintf("%q is not implemented yet", args[0]),
		}
	default:
		return &ExitError{
			Code:    2,
			Message: fmt.Sprintf("unknown command %q (run 'yapp help')", args[0]),
		}
	}
}

func installApp(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Message: "usage: yapp install <app>"}
	}

	shell, err := shellenv.Detect()
	if err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}
	yappHome := filepath.Join(userHome, ".yapp")
	catalogData, err := catalog.Default()
	if err != nil {
		return err
	}
	app, ok := catalogData.Apps[args[0]]
	if !ok {
		return &ExitError{Code: 2, Message: fmt.Sprintf("unknown app %q", args[0])}
	}
	artifact, err := app.ArtifactFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	current, err := state.Load(yappHome)
	if err != nil {
		return fmt.Errorf("read YAPP installation state: %w", err)
	}
	if installed, exists := current.Apps[args[0]]; exists {
		if installed.Version != app.Version || installed.Path != filepath.ToSlash(app.InstallPath) || installed.ArtifactURL != artifact.URL || installed.SHA256 != artifact.SHA256 {
			return fmt.Errorf("%s is already installed in a different state; upgrades are not implemented yet", app.Name)
		}
		for _, executable := range app.Executables {
			executablePath := filepath.Join(yappHome, filepath.FromSlash(installed.Path), filepath.FromSlash(executable))
			executableInfo, err := os.Stat(executablePath)
			if err != nil || !executableInfo.Mode().IsRegular() || executableInfo.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("installation state exists, but %s is missing or invalid at %s", app.Name, executablePath)
			}
		}
		if err := configureShell(userHome, shell, catalogData, current); err != nil {
			return fmt.Errorf("%s is installed, but shell configuration failed: %w", app.Name, err)
		}
		_, err = fmt.Fprintf(stdout, "%s %s is already installed; shell PATH configuration is ready. Restart your shell or source your rc file.\n", app.Name, app.Version)
		if err != nil {
			return err
		}
		return writeDependencyHints(stdout, yappHome, catalogData, app, current)
	}

	installRecord, installPath, err := installer.Install(ctx, yappHome, app, artifact)
	if err != nil {
		return err
	}
	if err := state.Record(yappHome, args[0], installRecord); err != nil {
		return fmt.Errorf("installed %s at %s, but could not record local state: %w", app.Name, installPath, err)
	}
	current.Apps[args[0]] = installRecord
	if err := configureShell(userHome, shell, catalogData, current); err != nil {
		return fmt.Errorf("installed %s, but could not configure shell PATH: %w", app.Name, err)
	}
	_, err = fmt.Fprintf(stdout, "Installed %s %s at %s. Updated shell environment and PATH setup in ~/.%src; restart your shell or source that file.\n", app.Name, app.Version, installPath, shell)
	if err != nil {
		return err
	}
	return writeDependencyHints(stdout, yappHome, catalogData, app, current)
}

func uninstallApp(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Message: "usage: yapp uninstall <app>"}
	}
	shell, err := shellenv.Detect()
	if err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}
	yappHome := filepath.Join(userHome, ".yapp")
	installed, err := state.Load(yappHome)
	if err != nil {
		return fmt.Errorf("read YAPP installation state: %w", err)
	}
	record, ok := installed.Apps[args[0]]
	if !ok {
		return &ExitError{Code: 1, Message: fmt.Sprintf("%q is not installed by YAPP", args[0])}
	}
	catalogData, err := catalog.Default()
	if err != nil {
		return err
	}
	if err := installer.Uninstall(yappHome, args[0], record); err != nil {
		return err
	}
	if err := state.Remove(yappHome, args[0]); err != nil {
		return fmt.Errorf("removed %s files but could not update local state: %w", record.Name, err)
	}
	delete(installed.Apps, args[0])
	if err := configureShell(userHome, shell, catalogData, installed); err != nil {
		return fmt.Errorf("uninstalled %s, but could not update shell configuration: %w", record.Name, err)
	}
	_, err = fmt.Fprintf(stdout, "Uninstalled %s %s. Updated shell PATH setup; restart your shell or source your rc file.\n", record.Name, record.Version)
	return err
}

func writeDependencyHints(w io.Writer, yappHome string, catalogData catalog.Catalog, app catalog.App, installed state.State) error {
	visited := make(map[catalog.Dependency]bool)
	var visit func(catalog.Dependency) error
	visit = func(requirement catalog.Dependency) error {
		if visited[requirement] {
			return nil
		}
		visited[requirement] = true
		dependency := catalogData.Apps[requirement.Name]
		for _, required := range dependency.DependsOn {
			if err := visit(required); err != nil {
				return err
			}
		}
		if reason := dependencyStatus(yappHome, requirement, dependency, installed); reason != "" {
			_, err := fmt.Fprintf(w, "Dependency %s (%s): %s. YAPP provides %s; install with: yapp install %s\n", requirement.Name, requirement.Version, reason, dependency.Version, requirement.Name)
			if err != nil {
				return fmt.Errorf("write dependency suggestion: %w", err)
			}
			if _, exists := installed.Apps[requirement.Name]; exists {
				_, err = fmt.Fprintf(w, "Replacing an existing YAPP installation currently requires: yapp uninstall %s, then yapp install %s\n", requirement.Name, requirement.Name)
				if err != nil {
					return fmt.Errorf("write dependency suggestion: %w", err)
				}
			}
		}
		return nil
	}
	for _, dependency := range app.DependsOn {
		if err := visit(dependency); err != nil {
			return err
		}
	}
	return nil
}

// An empty status means the recorded version satisfies the requirement.
// Finding a system executable alone cannot establish version compatibility.
func dependencyStatus(yappHome string, requirement catalog.Dependency, app catalog.App, installed state.State) string {
	if record, ok := installed.Apps[requirement.Name]; ok {
		for _, executable := range app.Executables {
			if !executableFile(filepath.Join(yappHome, filepath.FromSlash(record.Path), filepath.FromSlash(executable))) {
				return "recorded installation has missing executables"
			}
		}
		version := record.SemanticVersion
		if version == "" {
			version = record.Version
			// Older registries lack semanticVersion. Only reuse the catalog mapping
			// when the recorded release and artifact still match exactly.
			if record.Version == app.Version {
				for _, artifact := range app.Artifacts {
					if artifact.URL == record.ArtifactURL && artifact.SHA256 == record.SHA256 {
						version = app.MatchingVersion()
						break
					}
				}
			}
		}
		matches, err := versionconstraint.Match(version, requirement.Version)
		if err != nil {
			return fmt.Sprintf("recorded release %s has an unverified semantic version", record.Version)
		}
		if !matches {
			return fmt.Sprintf("installed version %s does not satisfy the requirement", version)
		}
		return ""
	}
	for _, executable := range app.Executables {
		if _, err := exec.LookPath(filepath.Base(executable)); err == nil {
			return "found on PATH, but its version is unverified"
		}
		for variable, rootPath := range app.Environment.Variables {
			root := os.Getenv(variable)
			if root == "" || rootPath != "." {
				continue
			}
			if executableFile(filepath.Join(root, filepath.FromSlash(executable))) {
				return "found through an environment variable, but its version is unverified"
			}
		}
	}
	return "not detected"
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func configureShell(userHome, shell string, catalogData catalog.Catalog, installed state.State) error {
	ids := make([]string, 0, len(installed.Apps))
	for id := range installed.Apps {
		ids = append(ids, id)
	}
	ids = catalogData.DependencyOrder(ids)
	environment := make(map[string]string)
	var pathEntries []string
	for _, id := range ids {
		app := catalogData.Apps[id]
		installPath := installed.Apps[id].Path
		for name, value := range app.Environment.Variables {
			if value == "." {
				value = installPath
			} else {
				value = filepath.ToSlash(filepath.Join(filepath.FromSlash(installPath), filepath.FromSlash(value)))
			}
			if previous, exists := environment[name]; exists && previous != value {
				return fmt.Errorf("installed apps configure conflicting values for environment variable %s", name)
			}
			environment[name] = value
		}
		for _, path := range app.Environment.Paths {
			pathEntries = append(pathEntries, filepath.ToSlash(filepath.Join(filepath.FromSlash(installPath), filepath.FromSlash(path))))
		}
	}
	return shellenv.Configure(userHome, shell, environment, pathEntries)
}

func writeHelp(w io.Writer) error {
	const help = `YAPP - Yet Another Package Provisioner

Usage:
  yapp <command> [arguments]

Available commands:
  install <app>  Download and install an app from the built-in catalog
  uninstall <app> Remove an app installed by YAPP
  help       Show this help
  version    Show version information

Planned commands (not implemented yet):
  list, info, search, update, outdated, upgrade, doctor, cleanup
`
	if _, err := io.WriteString(w, help); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
