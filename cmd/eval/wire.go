//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/eval"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/gencache"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
	"github.com/dphbfs/fast-resume-scoring/internal/wiring"
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

// initCheckerRunner builds the Resume Checker eval runner. The generative
// baseline arm runs when baseline is enabled; it calls the model directly
// (no cache) so its cost and latency are real.
func initCheckerRunner(baseline eval.BaselineConfig) (*eval.CheckerRunner, error) {
	wire.Build(wiring.PlatformSet, wiring.ClassifierSet, wiring.CheckerSet, openai.New, eval.NewBaseline, eval.NewCheckerRunner)
	return nil, nil
}

// initE2ERunner builds the end-to-end eval runner: extraction (Job
// Summaries through the file cache, as in initRunner) and checking.
func initE2ERunner(cacheDir gencache.Dir) (*eval.E2ERunner, error) {
	wire.Build(
		wiring.PlatformSet, wiring.ClassifierSet, wiring.ExtractorSet, wiring.CheckerSet, wiring.HolisticSet,
		openai.New, wire.Bind(new(gencache.Inner), new(*openai.Client)), gencache.New,
		wire.Bind(new(port.AIGenerativeClient), new(*gencache.Client)),
		eval.NewE2ERunner,
	)
	return nil, nil
}
