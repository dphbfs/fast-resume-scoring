// Package app implements the Requirement Extractor use case: the pipeline
// described in CLAUDE.md, built on the ports in internal/port.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// ErrNotImplemented marks pipeline stages that are still scaffolding.
var ErrNotImplemented = errors.New("not implemented")

// Extractor runs the Requirement Extractor pipeline.
type Extractor struct {
	classifier port.AIClassifierClient
	generator  port.AIGenerativeClient
	metrics    port.Metrics
	log        *slog.Logger
	cfg        config.Pipeline
}

var _ port.RequirementExtractor = (*Extractor)(nil)

// New builds an Extractor.
func New(
	classifier port.AIClassifierClient,
	generator port.AIGenerativeClient,
	m port.Metrics,
	log *slog.Logger,
	cfg config.Pipeline,
) *Extractor {
	return &Extractor{
		classifier: classifier,
		generator:  generator,
		metrics:    m,
		log:        log.With("component", "extractor"),
		cfg:        cfg,
	}
}

// run tracks one pipeline state across stages.
type run struct {
	jd         domain.JobDescription
	model      string // versioned Jev model that answered, e.g. "jev-1.13.0"
	sentences  []domain.ContextSentence
	headings   []string // headings[i] is the nearest heading above sentences[i]
	summary    string
	candidates []domain.Candidate
	accepted   []judged
	result     domain.Result
}

// Extract turns a Job Description into Requirements.
func (e *Extractor) Extract(ctx context.Context, jd domain.JobDescription) (domain.Result, error) {
	r := &run{jd: jd}
	stages := []struct {
		name string
		fn   func(context.Context, *run) error
	}{
		{"split_sentences", e.splitSentencesStage},
		{"label_sections", e.labelSections},
		{"generate_candidates", e.generateCandidates},
		{"job_summary", e.jobSummary},
		{"validation_round", e.validationRound},
		{"refinement_round", e.refinementRound},
	}
	for _, s := range stages {
		if err := e.stage(ctx, s.name, r, s.fn); err != nil {
			return domain.Result{}, err
		}
	}
	return r.result, nil
}

// stage runs fn with timing, metrics and structured logs.
func (e *Extractor) stage(ctx context.Context, name string, r *run, fn func(context.Context, *run) error) error {
	start := time.Now()
	err := fn(ctx, r)
	elapsed := time.Since(start)
	e.metrics.ObserveDuration("stage."+name, elapsed)
	if err != nil {
		e.log.ErrorContext(ctx, "stage failed", "stage", name, "duration_ms", elapsed.Milliseconds(), "error", err)
		return fmt.Errorf("%s: %w", name, err)
	}
	e.log.InfoContext(ctx, "stage done", "stage", name, "duration_ms", elapsed.Milliseconds())
	return nil
}

// refinementRound merges synonyms, builds Alternative Groups, drops Filler
// and assigns Importance in one batched Jev request.
func (e *Extractor) refinementRound(context.Context, *run) error { return ErrNotImplemented }
