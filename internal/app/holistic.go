package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
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
}

var _ port.HolisticJudge = (*HolisticJudge)(nil)

// NewHolisticJudge builds a HolisticJudge.
func NewHolisticJudge(classifier port.AIClassifierClient, m port.Metrics, log *slog.Logger) *HolisticJudge {
	return &HolisticJudge{classifier: classifier, metrics: m, log: log.With("component", "holistic")}
}

// holisticState is the Jev state: the posting as the extractor reads it and
// the Resume as written.
type holisticState struct {
	JobPosting string `json:"job_posting"`
	Resume     string `json:"resume"`
}

// roleMatchLevels rate how closely the candidate's kind of role matches
// the job's, lowest first.
var roleMatchLevels = []any{
	"A different profession or specialty (for example Android/mobile vs data engineering, Salesforce " +
		"development vs backend services, front-end vs data/ML).",
	"A neighboring specialty with little overlap in daily work.",
	"The same broad field with a different focus (for example front-end-leaning vs backend-leaning).",
	"The same kind of role with a different main stack.",
	"The same kind of role and stack.",
}

// holisticQuestions are the Holistic Round's questions, worded as in the
// probe that fitted the Match Score (scripts/probe_holistic.py); changing
// a word means refitting.
var holisticQuestions = map[string]port.Question{
	"role_match": {
		Type: port.Score,
		Instructions: "How closely does the kind of role the candidate in `resume` has been doing match the kind " +
			"of role in `job_posting`? Judge the role (what the person builds and which specialty), not " +
			"seniority or domain.",
		Criteria: roleMatchLevels,
	},
	"experience_short": {
		Type: port.Noul,
		Instructions: "Does `resume` show clearly fewer years of the relevant kind of experience, or a clearly " +
			"lower career level, than `job_posting` asks for?",
		Criteria: map[string]any{
			"true":  "The candidate's relevant experience or level is clearly below what the job asks for.",
			"false": "The candidate meets or exceeds the experience and level, or is within a year or so of it.",
		},
	},
	"blocker": {
		Type: port.Noul,
		Instructions: "Does `job_posting` state a hard eligibility condition that `resume` clearly shows the candidate " +
			"does not meet? Only status conditions count: must be a current student or recent graduate, must " +
			"hold a security clearance, a professional license, or citizenship the resume rules out. Skills, " +
			"years of experience, degrees, and location are qualifications, not eligibility conditions.",
		Criteria: map[string]any{
			"true": "A stated status condition (student, clearance, license, citizenship) is clearly not met.",
			"false": "No status condition is stated, it is met, or the resume does not clearly contradict it. " +
				"Missing years, a missing degree, or missing skills are never a yes.",
		},
	},
}

// Judge asks the Holistic Round questions for one posting and Resume.
func (h *HolisticJudge) Judge(ctx context.Context, jd domain.JobDescription, resume domain.Resume) (domain.Holistic, error) {
	start := time.Now()
	defer func() { h.metrics.ObserveDuration("stage.holistic", time.Since(start)) }()
	resp, err := h.classifier.Classify(ctx, port.ClassifyRequest{
		State:     holisticState{JobPosting: postingText(jd).Text, Resume: resume.Text},
		Questions: holisticQuestions,
	})
	if err != nil {
		return domain.Holistic{}, err
	}
	h.metrics.Add("holistic.requests", 1)
	h.metrics.Add("holistic.input_tokens", int64(resp.Usage.InputTokens))
	h.metrics.Add("holistic.output_tokens", int64(resp.Usage.OutputTokens))

	role, err := scoreShare(resp.Answers["role_match"], len(roleMatchLevels))
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
