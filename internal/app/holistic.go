package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

// HolisticJudge judges the whole Resume against the whole posting in one
// Jev request (the Holistic Round), which alone determines the Match Score
// (docs/adr/0004): how closely the candidate's kind of role matches the
// job's, whether the candidate is clearly under-qualified for it, and
// whether a stated status condition is clearly unmet. It needs no
// extracted Requirements. Questions tried and dropped (core work,
// responsibilities, domain closeness and mismatch, career level, role
// type, transferable scope, primary gap, soft eligibility, location, must-
// haves, employment gap) are in docs/tuning.md.
type HolisticJudge struct {
	classifier port.AIClassifierClient
	metrics    port.Metrics
	log        *slog.Logger
	prompts    *prompts
}

var _ port.HolisticJudge = (*HolisticJudge)(nil)

// NewHolisticJudge builds a HolisticJudge.
// The questions' wording comes from the tuning file (holistic section);
// the Match Score weights were fitted on it, so a change means refitting.
func NewHolisticJudge(classifier port.AIClassifierClient, m port.Metrics, log *slog.Logger, t *tuning.Tuning) *HolisticJudge {
	return &HolisticJudge{classifier: classifier, metrics: m, log: log.With("component", "holistic"), prompts: newPrompts(t)}
}

// holisticState is the Jev state: the posting as the extractor reads it and
// the Resume as written.
type holisticState struct {
	JobPosting string `json:"job_posting"`
	Resume     string `json:"resume"`
}

// Judge asks the Holistic Round questions for one posting and Resume.
func (h *HolisticJudge) Judge(ctx context.Context, jd domain.JobDescription, resume domain.Resume) (domain.Holistic, error) {
	start := time.Now()
	defer func() { h.metrics.ObserveDuration("stage.holistic", time.Since(start)) }()
	resp, err := h.classifier.Classify(ctx, port.ClassifyRequest{
		State:     holisticState{JobPosting: postingText(jd).Text, Resume: resume.Text},
		Questions: h.prompts.holistic,
	})
	if err != nil {
		return domain.Holistic{}, err
	}
	h.metrics.Add("holistic.requests", 1)
	h.metrics.Add("holistic.input_tokens", int64(resp.Usage.InputTokens))
	h.metrics.Add("holistic.output_tokens", int64(resp.Usage.OutputTokens))

	role, err := scoreShare(resp.Answers["role_match"], h.prompts.roleMatchLevels)
	if err != nil {
		return domain.Holistic{}, fmt.Errorf("role_match: %w", err)
	}
	nouls := map[string]float64{}
	for _, id := range []string{"experience_short", "blocker"} {
		p := resp.Answers[id].Noul
		if p == nil {
			return domain.Holistic{}, fmt.Errorf("%s: no noul answer", id)
		}
		nouls[id] = *p
	}
	return domain.Holistic{Model: resp.Model, RoleMatch: role, ExperienceShort: nouls["experience_short"], Blocker: nouls["blocker"]}, nil
}

// scoreShare is a Score answer as a share of its top level, 0..1.
func scoreShare(a port.Answer, levels int) (float64, error) {
	if a.Score == nil {
		return 0, fmt.Errorf("no score answer")
	}
	return *a.Score / float64(levels-1), nil
}
