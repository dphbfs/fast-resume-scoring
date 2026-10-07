// Command check runs the Resume Checker: it links a Resume's Evidence Units
// to the Requirements of an extract result and prints the coverage JSON
// (coverage schema v1).
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
		cli.NewCheckApp(nil, nil, nil, config.Run{}).Run(ctx, args, os.Stdout, os.Stderr)
		return cli.ExitOK
	}

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "check:", err)
		return cli.ExitError
	}
	return app.Run(ctx, args, os.Stdout, os.Stderr)
}
