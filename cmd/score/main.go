// Command score prints the Match Score of a Resume for a Job Description
// (match schema v1), from one Holistic Round request to Jev.
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
		cli.NewScoreApp(nil, nil, nil, config.Run{}, nil).Run(ctx, args, os.Stdout, os.Stderr)
		return cli.ExitOK
	}

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "score:", err)
		return cli.ExitError
	}
	return app.Run(ctx, args, os.Stdout, os.Stderr)
}
