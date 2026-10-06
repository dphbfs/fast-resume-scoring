package domain

import "math"

// Match Score weights (docs/adr/0004), fitted by least squares on the
// 50-pair development pool (testdata/reference subset plus testdata/final,
// 5 Resumes) from Holistic probe answers (2026-10-06T00-58-06Z), and
// checked with each Resume held out. Refit only on development data.
const (
	matchIntercept       = 80.7
	matchRoleMatch       = 18.5
	matchExperienceShort = -55.2
)

// MatchScore is the overall 0-100 match of a Resume to a posting, on the
// generative reference's scale, from the Holistic Round alone: role match,
// less an under-qualification penalty, discounted by the probability of a
// clearly unmet status condition.
func MatchScore(h Holistic) int {
	x := matchIntercept + matchRoleMatch*h.RoleMatch + matchExperienceShort*h.ExperienceShort
	return int(math.Round(min(100, max(0, x*(1-h.Blocker)))))
}

// MatchSchemaVersion is the version of the score output contract.
const MatchSchemaVersion = "1"

// MatchResult is the score output (match schema v1): the Match Score and
// the Holistic Round answers behind it.
type MatchResult struct {
	SchemaVersion string   `json:"schema_version"`
	Model         string   `json:"model"`
	MatchScore    int      `json:"match_score"`
	Signals       Holistic `json:"signals"`
}

// NewMatchResult builds the score output for a Holistic Round answer.
func NewMatchResult(h Holistic) MatchResult {
	signals := h
	signals.Model = ""
	return MatchResult{SchemaVersion: MatchSchemaVersion, Model: h.Model, MatchScore: MatchScore(h), Signals: signals}
}
