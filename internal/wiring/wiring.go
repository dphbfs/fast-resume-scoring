// Package wiring holds the google/wire provider sets shared by the
// command-line entry points (cmd/extract, cmd/eval).
package wiring

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-tailoring/internal/app"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/logging"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// PlatformSet provides config, logging and the metrics recorder.
var PlatformSet = wire.NewSet(
	config.Load,
	wire.FieldsOf(new(config.Config), "Jev", "Generative", "Pipeline"),
	logging.New,
	metrics.NewRecorder,
	wire.Bind(new(port.Metrics), new(*metrics.Recorder)),
)

// AIClientSet provides the Jev and generative clients behind their ports.
var AIClientSet = wire.NewSet(
	jev.New,
	wire.Bind(new(port.AIClassifierClient), new(*jev.Client)),
	openai.New,
	wire.Bind(new(port.AIGenerativeClient), new(*openai.Client)),
)

// ExtractorSet provides the Requirement Extractor behind its driving port.
var ExtractorSet = wire.NewSet(
	app.New,
	wire.Bind(new(port.RequirementExtractor), new(*app.Extractor)),
)
