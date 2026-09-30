//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/cli"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-tailoring/internal/app"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/logging"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

var platformSet = wire.NewSet(
	config.Load,
	wire.FieldsOf(new(config.Config), "Jev", "Generative", "Pipeline"),
	logging.New,
	metrics.NewRecorder,
	wire.Bind(new(port.Metrics), new(*metrics.Recorder)),
)

var adapterSet = wire.NewSet(
	jev.New,
	wire.Bind(new(port.AIClassifierClient), new(*jev.Client)),
	openai.New,
	wire.Bind(new(port.AIGenerativeClient), new(*openai.Client)),
	cli.New,
)

var appSet = wire.NewSet(
	app.New,
	wire.Bind(new(port.RequirementExtractor), new(*app.Extractor)),
)

// initApp builds the CLI with every dependency wired from the environment.
func initApp() (*cli.App, error) {
	wire.Build(platformSet, adapterSet, appSet)
	return nil, nil
}
