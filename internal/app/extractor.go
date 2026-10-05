// Package app implements the Requirement Extractor and Resume Checker use
// cases described in CLAUDE.md, built on the ports in internal/port.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// Extractor runs the Requirement Extractor pipeline.
type Extractor struct {
	classifier port.AIClassifierClient
	generator  port.AIGenerativeClient
	metrics    port.Metrics
	log        *slog.Logger
	cfg        config.Pipeline
	// refinementBatchChars caps question JSON per Refinement request;
	// replaced in tests.
	refinementBatchChars int
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

		refinementBatchChars: refinementBatchChars,
	}
}

// run tracks one pipeline state across stages.
type run struct {
	jd        domain.JobDescription
	model     string // versioned Jev model that answered, e.g. "jev-1.13.0"
	sentences []domain.ContextSentence
	headings  []string // headings[i] is the nearest heading above sentences[i]
	// strongHeading[i] marks sentences[i] as a markdown or colon-terminated
	// heading, which Candidate generation skips.
	strongHeading []bool
	// sectionConf[i] is Jev's confidence in sentences[i].Section.
	sectionConf []float64
	summary     string
	chunks      []chunk
	accepted    []judged
	// Refinement Round output, keyed by lowercase Requirement value.
	importance map[string]float64
	groups     [][]string
	result     domain.Result
	trace      domain.Trace
}

// Extract turns a Job Description into Requirements.
func (e *Extractor) Extract(ctx context.Context, jd domain.JobDescription) (domain.Result, domain.Trace, error) {
	r := &run{jd: postingText(jd)}
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
		{"build_result", e.buildResult},
	}
	for _, s := range stages {
		if err := e.stage(ctx, s.name, r, s.fn); err != nil {
			return domain.Result{}, r.trace, err
		}
	}
	return r.result, r.trace, nil
}

// stage runs fn with timing, metrics and structured logs.
// addUsage records one Jev call's tokens under its stage.
func (e *Extractor) addUsage(stage string, u port.Usage) {
	e.metrics.Add(stage+".input_tokens", int64(u.InputTokens))
	e.metrics.Add(stage+".output_tokens", int64(u.OutputTokens))
}

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
