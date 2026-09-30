// Command extract runs the Requirement Extractor on one Job Description file
// and prints the result JSON (schema v1).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		return cli.ExitError
	}
	return app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
