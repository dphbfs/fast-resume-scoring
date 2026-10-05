package app

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// HolisticJudge judges the whole Resume against the whole posting in one
// Jev request (the Holistic Round): how much of the job's day-to-day
// responsibilities the candidate has carried out, whether the job's domain
// is new to the candidate, whether a stated hard eligibility condition
// other than location is clearly unmet, whether the job's primary
// technology is missing, and whether the candidate is outside every
// allowed location; plus the employment gap, from the Resume's dates in
// code. It needs no extracted Requirements, so it runs alongside
// extraction and checking. Questions tried and dropped (core work, domain
// closeness, career level, role type, transferable scope, soft
// eligibility, a blocker that included location) are in docs/tuning.md.
type HolisticJudge struct {
	classifier port.AIClassifierClient
	metrics    port.Metrics
	log        *slog.Logger
	now        func() time.Time // the as-of date for the employment gap
}

var _ port.HolisticJudge = (*HolisticJudge)(nil)

// NewHolisticJudge builds a HolisticJudge.
func NewHolisticJudge(classifier port.AIClassifierClient, m port.Metrics, log *slog.Logger) *HolisticJudge {
	return &HolisticJudge{classifier: classifier, metrics: m, log: log.With("component", "holistic"), now: time.Now}
}

// holisticState is the Jev state: the posting as the extractor reads it and
// the Resume as written.
type holisticState struct {
	JobPosting string `json:"job_posting"`
	Resume     string `json:"resume"`
}

// responsibilityLevels rate how much of the job's day-to-day
// responsibilities the candidate has carried out, in any domain, lowest
// first.
var responsibilityLevels = []any{
	"None of the job's day-to-day responsibilities appear in the candidate's work.",
	"A few of them, in a limited way.",
	"About half of them.",
	"Most of them.",
	"Nearly all of them, at the scope the job describes.",
}

// Judge asks the Holistic Round questions for one posting and Resume.
func (h *HolisticJudge) Judge(ctx context.Context, jd domain.JobDescription, resume domain.Resume) (domain.Holistic, error) {
	start := time.Now()
	defer func() { h.metrics.ObserveDuration("stage.holistic", time.Since(start)) }()
	resp, err := h.classifier.Classify(ctx, port.ClassifyRequest{
		State: holisticState{JobPosting: postingText(jd).Text, Resume: resume.Text},
		Questions: map[string]port.Question{
			"responsibilities": {
				Type: port.Score,
				Instructions: "Regardless of product or industry domain, how much of the day-to-day responsibilities " +
					"in `job_posting` (what the person will build, own, and operate) has the candidate in `resume` carried out?",
				Criteria: responsibilityLevels,
			},
			"primary_gap": {
				Type: port.Noul,
				Instructions: "Is the primary technology or core skill that `job_posting` centers on (its main " +
					"programming language, platform, or specialty) absent from `resume`?",
				Criteria: map[string]any{
					"true":  "The job's main language, platform, or specialty does not appear in the candidate's work or skills.",
					"false": "The candidate has worked with the job's main language, platform, or specialty.",
				},
			},
			"domain_mismatch": {
				Type: port.Noul,
				Instructions: "Is the product or industry domain of `job_posting` (for example security, payments, " +
					"identity, healthcare) one the candidate in `resume` has never worked in?",
				Criteria: map[string]any{
					"true":  "The candidate's work shows no experience in the job's product or industry domain.",
					"false": "The candidate has worked in the job's domain or a closely related one, or the job has no specific domain.",
				},
			},
			"blocker": {
				Type: port.Noul,
				Instructions: "Does `job_posting` state a hard eligibility condition other than location (for example: must " +
					"be a current student, must hold a security clearance or license) that `resume` clearly shows the " +
					"candidate does not meet?",
				Criteria: map[string]any{
					"true": "A stated non-location condition is clearly not met by what the resume shows.",
					"false": "No such condition is stated, or the resume does not clearly contradict it (a condition the " +
						"resume does not mention, such as citizenship, is not clearly unmet).",
				},
			},
			"location_mismatch": {
				Type: port.Noul,
				Instructions: "Compare the locations `job_posting` allows (remote countries or regions, office cities) with " +
					"the candidate's location stated in `resume`. Is the candidate outside every allowed location?",
				Criteria: map[string]any{
					"true": "The candidate's stated location is outside every location the posting allows (for example an " +
						"on-site or hybrid city elsewhere).",
					"false": "The candidate's location is allowed (for example a remote role open to their country), or " +
						"the resume states no location.",
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

	resp2, err := scoreShare(resp.Answers["responsibilities"], len(responsibilityLevels))
	if err != nil {
		return domain.Holistic{}, fmt.Errorf("responsibilities: %w", err)
	}
	nouls := map[string]float64{}
	for _, id := range []string{"blocker", "primary_gap", "domain_mismatch", "location_mismatch"} {
		p := resp.Answers[id].Noul
		if p == nil {
			return domain.Holistic{}, fmt.Errorf("%s: no noul answer", id)
		}
		nouls[id] = *p
	}
	return domain.Holistic{
		Model: resp.Model, Blocker: nouls["blocker"], Responsibilities: resp2,
		PrimaryGap: nouls["primary_gap"], DomainMismatch: nouls["domain_mismatch"], LocationMismatch: nouls["location_mismatch"],
		GapMonths: employmentGapMonths(ParseResume(resume.Text), h.now()),
	}, nil
}

// endDate matches the end of a date range such as "Jun 2025 – Nov 2025",
// "2009 – 2013", or "Mar 2022 - Present".
var endDate = regexp.MustCompile(`(?i)[-–—]\s*(?:([a-z]{3,9})\.?\s+)?(\d{4}|present|current|now)\s*$`)

// employmentGapMonths is the whole months between the latest end date of
// the Resume's roles and asOf: 0 when a role is current, nil when no role
// has a parsable end date. A year without a month counts as December.
func employmentGapMonths(units []domain.EvidenceUnit, asOf time.Time) *int {
	var latest time.Time
	seen := map[string]bool{}
	for _, u := range units {
		if u.Dates == "" || u.ResumeSection != domain.ResumeExperience || seen[u.Dates] {
			continue
		}
		seen[u.Dates] = true
		m := endDate.FindStringSubmatch(strings.TrimSpace(u.Dates))
		if m == nil {
			continue
		}
		switch strings.ToLower(m[2]) {
		case "present", "current", "now":
			zero := 0
			return &zero
		}
		year, _ := strconv.Atoi(m[2])
		month := time.December
		if m[1] != "" {
			if t, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:3])); err == nil {
				month = t.Month()
			}
		}
		if end := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC); end.After(latest) {
			latest = end
		}
	}
	if latest.IsZero() {
		return nil
	}
	months := (asOf.Year()-latest.Year())*12 + int(asOf.Month()) - int(latest.Month())
	months = max(0, months)
	return &months
}

// scoreShare is a Score answer as a share of its top level, 0..1.
func scoreShare(a port.Answer, levels int) (float64, error) {
	if a.Score == nil {
		return 0, fmt.Errorf("no score answer")
	}
	return *a.Score / float64(levels-1), nil
}
