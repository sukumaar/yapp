// Package cli implements YAPP's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sukumaar/yapp/internal/buildinfo"
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

// Execute handles the implemented CLI commands. Package-management commands
// are intentionally reported as unavailable until their behavior is implemented.
func Execute(ctx context.Context, args []string, stdout io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("operation canceled: %w", err)
	}

	if len(args) == 0 {
		return writeHelp(stdout)
	}

	switch args[0] {
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
	case "install", "uninstall", "list", "info", "search", "update", "outdated", "upgrade", "doctor", "cleanup":
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

func writeHelp(w io.Writer) error {
	const help = `YAPP - Yet Another Package Provisioner

Usage:
  yapp <command> [arguments]

Available commands:
  help       Show this help
  version    Show version information

Planned commands (not implemented yet):
  install, uninstall, list, info, search, update, outdated,
  upgrade, doctor, cleanup
`
	if _, err := io.WriteString(w, help); err != nil {
		return fmt.Errorf("write help: %w", err)
	}
	return nil
}
