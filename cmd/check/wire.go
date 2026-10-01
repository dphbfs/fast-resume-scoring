//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-tailoring/internal/wiring"
)

// initApp builds the check CLI with every dependency wired from the
// environment.
func initApp() (*cli.CheckApp, error) {
	wire.Build(wiring.PlatformSet, wiring.ClassifierSet, wiring.CheckerSet, cli.NewCheckApp)
	return nil, nil
}
