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
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "check:", err)
		return cli.ExitError
	}
	return app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
