// Package wiring holds the google/wire provider sets shared by the
// command-line entry points (cmd/extract, cmd/check, cmd/score, cmd/eval).
package wiring

import (
	"log/slog"

	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev/replay"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/openai"
	"github.com/dphbfs/fast-resume-scoring/internal/app"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/logging"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
	"github.com/dphbfs/fast-resume-scoring/tuning"
)

// PlatformSet provides config, logging and the metrics recorder.
var PlatformSet = wire.NewSet(
	config.Load,
	wire.FieldsOf(new(config.Config), "Jev", "Generative", "Pipeline", "Checker", "Run", "Tuning"),
	tuning.Load,
	logging.New,
	metrics.NewRecorder,
	wire.Bind(new(port.Metrics), new(*metrics.Recorder)),
)

// ClassifierSet provides the Jev client behind its port.
var ClassifierSet = wire.NewSet(NewClassifier)

// NewClassifier picks the classifier the config asks for: the Jev API, the
// API with every exchange recorded (JEV_RECORD), or a recording replayed
// with no network or key (JEV_REPLAY).
func NewClassifier(cfg config.Jev, m port.Metrics, log *slog.Logger) (port.AIClassifierClient, error) {
	if cfg.ReplayFile != "" {
		log.Info("answering Jev from a recording; no API calls", "file", cfg.ReplayFile)
		return replay.NewPlayer(cfg.ReplayFile, m)
	}
	client, err := jev.New(cfg, m, log)
	if err != nil {
		return nil, err
	}
	if cfg.RecordFile != "" {
		log.Info("recording Jev exchanges", "file", cfg.RecordFile)
		return replay.NewRecorder(client, cfg.RecordFile)
	}
	return client, nil
}

// AIClientSet provides the Jev and generative clients behind their ports.
var AIClientSet = wire.NewSet(
	ClassifierSet,
	openai.New,
	wire.Bind(new(port.AIGenerativeClient), new(*openai.Client)),
)

// ExtractorSet provides the Requirement Extractor behind its driving port.
var ExtractorSet = wire.NewSet(
	app.New,
	wire.Bind(new(port.RequirementExtractor), new(*app.Extractor)),
)

// CheckerSet provides the Resume Checker behind its driving port.
var CheckerSet = wire.NewSet(
	app.NewChecker,
	wire.Bind(new(port.ResumeChecker), new(*app.Checker)),
)

// HolisticSet provides the Holistic Round judge behind its driving port.
var HolisticSet = wire.NewSet(
	app.NewHolisticJudge,
	wire.Bind(new(port.HolisticJudge), new(*app.HolisticJudge)),
)
