// Command yapp is the entry point for the YAPP CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sukumaar/yapp/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli.Execute(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "yapp:", err)
		return cli.ExitCode(err)
	}
	return 0
}
