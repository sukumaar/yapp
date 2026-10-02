// Package cli implements YAPP's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sukumaar/yapp/internal/buildinfo"
	"github.com/sukumaar/yapp/internal/catalog"
	"github.com/sukumaar/yapp/internal/installer"
	"github.com/sukumaar/yapp/internal/shellenv"
	"github.com/sukumaar/yapp/internal/state"
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
	case "uninstall", "list", "info", "search", "update", "outdated", "upgrade", "doctor", "cleanup":
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
		javaPath := filepath.Join(yappHome, filepath.FromSlash(installed.Path), "bin", "java")
		javaInfo, err := os.Stat(javaPath)
		if err != nil || !javaInfo.Mode().IsRegular() || javaInfo.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("installation state exists, but %s is missing or invalid at %s", app.Name, javaPath)
		}
		if err := shellenv.Configure(userHome, shell, app.InstallPath); err != nil {
			return fmt.Errorf("%s is installed, but shell configuration failed: %w", app.Name, err)
		}
		_, err = fmt.Fprintf(stdout, "%s %s is already installed; shell PATH configuration is ready. Restart your shell or source your rc file.\n", app.Name, app.Version)
		return err
	}

	installRecord, installPath, err := installer.Install(ctx, yappHome, args[0], app, artifact)
	if err != nil {
		return err
	}
	if err := state.Record(yappHome, args[0], installRecord); err != nil {
		return fmt.Errorf("installed %s at %s, but could not record local state: %w", app.Name, installPath, err)
	}
	if err := shellenv.Configure(userHome, shell, app.InstallPath); err != nil {
		return fmt.Errorf("installed %s, but could not configure shell PATH: %w", app.Name, err)
	}
	_, err = fmt.Fprintf(stdout, "Installed %s %s at %s. Added JAVA_HOME and PATH setup to ~/.%src; restart your shell or source that file.\n", app.Name, app.Version, installPath, shell)
	return err
}

func writeHelp(w io.Writer) error {
	const help = `YAPP - Yet Another Package Provisioner

Usage:
  yapp <command> [arguments]

Available commands:
  install <app>  Download and install an app from the built-in catalog
  help       Show this help
  version    Show version information

Planned commands (not implemented yet):
  uninstall, list, info, search, update, outdated, upgrade,
  doctor, cleanup
`
	if _, err := io.WriteString(w, help); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
