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
// Jev request (the Holistic Round): how much of the job's core work the
// candidate has done, and whether a stated hard eligibility condition is
// clearly unmet. (Product-domain and career-level questions were tried and
// dropped: they did not track the reference; docs/tuning.md.) It needs no extracted Requirements, so
// it can run alongside extraction and checking.
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

// coreWorkLevels rate how much of the job's core work the candidate has
// done, lowest first.
var coreWorkLevels = []any{
	"The candidate has done none of the job's core work; their experience is in another field.",
	"The candidate's work is adjacent: the same broad field, but different core tasks or technologies.",
	"The candidate has done some of the job's core work, but its main tasks or main technologies are missing.",
	"The candidate has done most of the job's core work, with a few main tasks or technologies missing.",
	"The candidate has repeatedly done the job's core work itself, with its main technologies.",
}

// Judge asks the Holistic Round questions for one posting and Resume.
func (h *HolisticJudge) Judge(ctx context.Context, jd domain.JobDescription, resume domain.Resume) (domain.Holistic, error) {
	start := time.Now()
	defer func() { h.metrics.ObserveDuration("stage.holistic", time.Since(start)) }()
	resp, err := h.classifier.Classify(ctx, port.ClassifyRequest{
		State: holisticState{JobPosting: postingText(jd).Text, Resume: resume.Text},
		Questions: map[string]port.Question{
			"core_work": {
				Type:         port.Score,
				Instructions: "How much of the core work described in `job_posting` has the candidate in `resume` done themselves?",
				Criteria:     coreWorkLevels,
			},
			"blocker": {
				Type: port.Noul,
				Instructions: "Does `job_posting` state a hard eligibility condition that `resume` clearly shows the candidate " +
					"does not meet (for example: must be a current student, must hold a license the candidate lacks)?",
				Criteria: map[string]any{
					"true": "A stated hard condition is clearly not met by what the resume shows.",
					"false": "No hard condition is stated, or the resume does not clearly contradict it " +
						"(a condition the resume simply does not mention, such as citizenship, is not a blocker).",
				},
			},
		},
	})
	if err != nil {
		return domain.Holistic{}, err
	}
	h.metrics.Add("holistic.requests", 1)
	h.metrics.Add("holistic.input_tokens", int64(resp.Usage.InputTokens))
	h.metrics.Add("holistic.output_tokens", int64(resp.Usage.OutputTokens))

	core, err := scoreShare(resp.Answers["core_work"], len(coreWorkLevels))
	if err != nil {
		return domain.Holistic{}, fmt.Errorf("core_work: %w", err)
	}
	blocker := resp.Answers["blocker"].Noul
	if blocker == nil {
		return domain.Holistic{}, fmt.Errorf("blocker: no noul answer")
	}
	return domain.Holistic{Model: resp.Model, CoreWork: core, Blocker: *blocker}, nil
}

// scoreShare is a Score answer as a share of its top level, 0..1.
func scoreShare(a port.Answer, levels int) (float64, error) {
	if a.Score == nil {
		return 0, fmt.Errorf("no score answer")
	}
	return *a.Score / float64(levels-1), nil
}
