// Command extract runs the Requirement Extractor on one Job Description file
// and prints the result JSON (schema v1).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Usage needs no config, so -h works without an API key.
	if cli.WantsHelp(args) {
		cli.New(nil, nil, nil, config.Run{}).Run(ctx, args, os.Stdout, os.Stderr)
		return cli.ExitOK
	}

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		return cli.ExitError
	}
	return app.Run(ctx, args, os.Stdout, os.Stderr)
}
