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
	"slices"
	"sort"
	"strings"

	"github.com/sukumaar/yapp/internal/buildinfo"
	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/commandlinks"
	"github.com/sukumaar/yapp/internal/installer"
	"github.com/sukumaar/yapp/internal/progressui"
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
		return uninstallApp(ctx, args[1:], stdout)
	case "info":
		return infoApp(args[1:], stdout)
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
	case "list", "search", "update", "outdated", "upgrade", "doctor", "cleanup":
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
	appID, app, ok := catalogData.ResolveApp(args[0])
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
	err = progressui.Run(ctx, stdout, "Installing "+app.Name+" "+app.Version, func(report progressui.Reporter) error {
		if installed, exists := current.Apps[appID]; exists {
			report("Checking existing installation", 0, 0)
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
			report("Creating command links", 0, 0)
			if err := commandlinks.Link(yappHome, installed.Path, app.LinkedBinaries()); err != nil {
				return fmt.Errorf("%s is installed, but command linking failed: %w", app.Name, err)
			}
			var obsolete []string
			for _, old := range installed.LinkedBinaries {
				if !slices.Contains(app.LinkedBinaries(), old) {
					obsolete = append(obsolete, old)
				}
			}
			if err := commandlinks.Remove(yappHome, installed.Path, obsolete); err != nil {
				return err
			}
			installed.LinkedBinaries = app.LinkedBinaries()
			if err := state.Record(yappHome, appID, installed); err != nil {
				return err
			}
			current.Apps[appID] = installed
			report("Updating shell environment", 0, 0)
			if err := configureShell(userHome, shell, catalogData, current, report); err != nil {
				return fmt.Errorf("%s is installed, but shell configuration failed: %w", app.Name, err)
			}
			report(fmt.Sprintf("%s %s is already installed; reload ~/.%src for initial setup or changed environment variables", app.Name, app.Version, shell), 0, 0)
			return reportDependencyHints(report, yappHome, catalogData, app, current)
		}

		installRecord, installPath, err := installer.Install(ctx, yappHome, appID, app, artifact, report)
		if err != nil {
			return err
		}
		report("Recording installation state", 0, 0)
		if err := state.Record(yappHome, appID, installRecord); err != nil {
			return fmt.Errorf("installed %s at %s, but could not record local state: %w", app.Name, installPath, err)
		}
		current.Apps[appID] = installRecord
		report("Creating command links", 0, 0)
		if err := commandlinks.Link(yappHome, installRecord.Path, installRecord.LinkedBinaries); err != nil {
			return fmt.Errorf("installed %s, but command linking failed: %w", app.Name, err)
		}
		report("Updating shell environment", 0, 0)
		if err := configureShell(userHome, shell, catalogData, current, report); err != nil {
			return fmt.Errorf("installed %s, but could not configure shell PATH: %w", app.Name, err)
		}
		report(fmt.Sprintf("Installed %s %s at %s. Reload ~/.%src for initial setup or changed environment variables", app.Name, app.Version, installPath, shell), 0, 0)
		return reportDependencyHints(report, yappHome, catalogData, app, current)
	})
	if err != nil {
		return err
	}
	cleanupPackageCache(ctx, yappHome, stdout)
	return nil
}

func cleanupPackageCache(ctx context.Context, yappHome string, stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout, "==> Checking package cache (archives older than 30 days expire).")
	removed, err := installer.CleanupCache(ctx, yappHome, func(message string) {
		_, _ = fmt.Fprintln(stdout, "    "+message)
	})
	if errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(stdout, "    Cache cleanup stopped; remaining archives will be checked after a later install.")
		return
	}
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "==> Could not clean package cache: %v\n", err)
		return
	}
	if removed == 0 {
		_, _ = fmt.Fprintln(stdout, "    No expired cached archives removed.")
	}
}

func reportDependencyHints(report progressui.Reporter, yappHome string, catalogData catalog.Catalog, app catalog.App, installed state.State) error {
	var hints strings.Builder
	if err := writeDependencyHints(&hints, yappHome, catalogData, app, installed); err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSuffix(hints.String(), "\n"), "\n") {
		if line != "" {
			report(line, 0, 0)
		}
	}
	return nil
}

func uninstallApp(ctx context.Context, args []string, stdout io.Writer) error {
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
	catalogData, err := catalog.Default()
	if err != nil {
		return err
	}
	appID, app, ok := catalogData.ResolveApp(args[0])
	if !ok {
		return &ExitError{Code: 2, Message: fmt.Sprintf("unknown app %q", args[0])}
	}
	record, ok := installed.Apps[appID]
	if !ok {
		return &ExitError{Code: 1, Message: fmt.Sprintf("%q is not installed by YAPP", args[0])}
	}
	return progressui.Run(ctx, stdout, "Uninstalling "+record.Name+" "+record.Version, func(report progressui.Reporter) error {
		report("Removing command links", 0, 0)
		if err := commandlinks.Remove(yappHome, record.Path, record.LinkedBinaries); err != nil {
			return fmt.Errorf("could not remove %s command links: %w", record.Name, err)
		}
		report("Removing installed files", 0, 0)
		if err := installer.Uninstall(yappHome, app, record); err != nil {
			return err
		}
		report("Removing installation state", 0, 0)
		if err := state.Remove(yappHome, appID); err != nil {
			return fmt.Errorf("removed %s files but could not update local state: %w", record.Name, err)
		}
		delete(installed.Apps, appID)
		report("Updating shell environment", 0, 0)
		if err := configureShell(userHome, shell, catalogData, installed, report); err != nil {
			return fmt.Errorf("uninstalled %s, but could not update shell configuration: %w", record.Name, err)
		}
		report(fmt.Sprintf("Uninstalled %s %s; shell PATH setup updated", record.Name, record.Version), 0, 0)
		return nil
	})
}

func infoApp(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Message: "usage: yapp info <app>"}
	}
	catalogData, err := catalog.Default()
	if err != nil {
		return err
	}
	appID, app, ok := catalogData.ResolveApp(args[0])
	if !ok {
		return &ExitError{Code: 2, Message: fmt.Sprintf("unknown app %q", args[0])}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}
	installed, err := state.Load(filepath.Join(userHome, ".yapp"))
	if err != nil {
		return fmt.Errorf("read YAPP installation state: %w", err)
	}
	record, isInstalled := installed.Apps[appID]
	return writeAppInfo(stdout, appID, app, record, isInstalled)
}

func writeAppInfo(w io.Writer, id string, app catalog.App, installed state.Install, isInstalled bool) error {
	var output strings.Builder
	fmt.Fprintf(&output, "==> %s: %s %s\n", id, app.Name, app.Version)
	if isInstalled {
		fmt.Fprintf(&output, "Installed:    yes (%s) — ~/.yapp/%s\n", installed.Version, installed.Path)
	} else {
		output.WriteString("Installed:    no\n")
	}
	fmt.Fprintf(&output, "Install path: ~/.yapp/%s\n", app.InstallPath)

	commands := append([]string(nil), app.LinkedBinaries()...)
	if len(commands) == 0 {
		commands = append(commands, app.Executables...)
	}
	for i := range commands {
		commands[i] = filepath.Base(commands[i])
	}
	sort.Strings(commands)
	fmt.Fprintf(&output, "Commands:     %s\n", strings.Join(commands, ", "))
	if app.ReleaseURL != "" {
		fmt.Fprintf(&output, "Release:      %s\n", app.ReleaseURL)
	}
	if len(app.DependsOn) > 0 {
		output.WriteString("Dependencies:\n")
		for _, dependency := range app.DependsOn {
			fmt.Fprintf(&output, "  %s %s\n", dependency.Name, dependency.Version)
		}
	}
	if len(app.Artifacts) > 0 {
		output.WriteString("Downloads:\n")
		for _, artifact := range app.Artifacts {
			fmt.Fprintf(&output, "  %s/%s (%s)\n    %s\n    SHA-256: %s\n", artifact.OS, artifact.Arch, artifact.Format, artifact.URL, artifact.SHA256)
		}
	}
	if len(app.Environment.Variables) > 0 {
		output.WriteString("Environment:\n")
		variables := make([]string, 0, len(app.Environment.Variables))
		for name := range app.Environment.Variables {
			variables = append(variables, name)
		}
		sort.Strings(variables)
		for _, name := range variables {
			value := app.Environment.Variables[name]
			if value == "." {
				value = "<install path>"
			}
			fmt.Fprintf(&output, "  %s=%s\n", name, value)
		}
	}
	if len(app.Environment.Paths) > 0 {
		fmt.Fprintf(&output, "PATH entries: %s\n", strings.Join(app.Environment.Paths, ", "))
	}
	if _, err := io.WriteString(w, output.String()); err != nil {
		return fmt.Errorf("write app info: %w", err)
	}
	return nil
}

func writeDependencyHints(w io.Writer, yappHome string, catalogData catalog.Catalog, app catalog.App, installed state.State) error {
	visited := make(map[catalog.Dependency]bool)
	var visit func(catalog.Dependency) error
	visit = func(requirement catalog.Dependency) error {
		if visited[requirement] {
			return nil
		}
		visited[requirement] = true
		providerID, dependency, err := catalogData.ResolveDependency(requirement)
		if err != nil {
			return fmt.Errorf("resolve dependency %q: %w", requirement.Name, err)
		}
		for _, required := range dependency.DependsOn {
			if err := visit(required); err != nil {
				return err
			}
		}
		if reason := dependencyStatus(yappHome, providerID, requirement, dependency, installed); reason != "" {
			_, err := fmt.Fprintf(w, "Dependency %s (%s): %s. YAPP provides %s; install with: yapp install %s\n", requirement.Name, requirement.Version, reason, dependency.Version, providerID)
			if err != nil {
				return fmt.Errorf("write dependency suggestion: %w", err)
			}
			if _, exists := installed.Apps[providerID]; exists {
				_, err = fmt.Fprintf(w, "Replacing an existing YAPP installation currently requires: yapp uninstall %s, then yapp install %s\n", providerID, providerID)
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
func dependencyStatus(yappHome, providerID string, requirement catalog.Dependency, app catalog.App, installed state.State) string {
	if record, ok := installed.Apps[providerID]; ok {
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

func configureShell(userHome, shell string, catalogData catalog.Catalog, installed state.State, report progressui.Reporter) error {
	variables, pathEntries, err := shellSettings(catalogData, installed)
	if err != nil {
		return err
	}
	return shellenv.Configure(userHome, shell, variables, pathEntries, func(message string) {
		report(message, 0, 0)
	})
}

func shellSettings(catalogData catalog.Catalog, installed state.State) (map[string]string, []string, error) {
	ids := make([]string, 0, len(installed.Apps))
	for id := range installed.Apps {
		ids = append(ids, id)
	}
	ids = catalogData.DependencyOrder(ids)
	environment := make(map[string]string)
	pathEntries := []string{"bin"}
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
				return nil, nil, fmt.Errorf("installed apps configure conflicting values for environment variable %s", name)
			}
			environment[name] = value
		}
		if app.InstallMode == nil {
			for _, path := range app.Environment.Paths {
				pathEntries = append(pathEntries, filepath.ToSlash(filepath.Join(filepath.FromSlash(installPath), filepath.FromSlash(path))))
			}
		}
	}
	return environment, pathEntries, nil
}

func writeHelp(w io.Writer) error {
	const help = `YAPP - Yet Another Package Provisioner

Usage:
  yapp <command> [arguments]

Available commands:
  install <app>  Download and install an app from the built-in catalog
  uninstall <app> Remove an app installed by YAPP
  info <app>     Show catalog, download, and installation details
  help           Show this help
  version        Show version information

Planned commands (not implemented yet):
  list, search, update, outdated, upgrade, doctor, cleanup
`
	if _, err := io.WriteString(w, help); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
