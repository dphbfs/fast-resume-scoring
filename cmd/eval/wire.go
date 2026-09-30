//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/eval"
	"github.com/dphbfs/fast-resume-tailoring/internal/wiring"
)

// initRunner builds the eval Runner with every dependency wired from the
// environment.
func initRunner() (*eval.Runner, error) {
	wire.Build(wiring.PlatformSet, wiring.AIClientSet, wiring.ExtractorSet, eval.NewRunner)
	return nil, nil
}
