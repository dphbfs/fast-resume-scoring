//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/eval"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/gencache"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
	"github.com/dphbfs/fast-resume-tailoring/internal/wiring"
)

// initRunner builds the eval Runner. Job Summaries go through a file cache
// so every eval run sees the same Validation inputs.
func initRunner(cacheDir gencache.Dir) (*eval.Runner, error) {
	wire.Build(
		wiring.PlatformSet, wiring.ClassifierSet, wiring.ExtractorSet,
		openai.New, wire.Bind(new(gencache.Inner), new(*openai.Client)), gencache.New,
		wire.Bind(new(port.AIGenerativeClient), new(*gencache.Client)),
		eval.NewRunner,
	)
	return nil, nil
}

// initCheckerRunner builds the Resume Checker eval runner.
func initCheckerRunner() (*eval.CheckerRunner, error) {
	wire.Build(wiring.PlatformSet, wiring.ClassifierSet, wiring.CheckerSet, eval.NewCheckerRunner)
	return nil, nil
}
