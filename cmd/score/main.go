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
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "score:", err)
		return cli.ExitError
	}
	return app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
