package domain

import "math"

// MatchWeights are the Match Score weights (docs/adr/0004), read from the
// tuning file (tuning/tuning.yaml). The shipped values were fitted by least
// squares on the 50-pair development pool from Holistic probe answers and
// checked with each Resume held out; refit only on development data.
type MatchWeights struct {
	Intercept       float64
	RoleMatch       float64
	ExperienceShort float64
}

// MatchScore is the overall 0-100 match of a Resume to a posting, on the
// generative reference's scale, from the Holistic Round alone: role match,
// less an under-qualification penalty, discounted by the probability of a
// clearly unmet status condition.
func MatchScore(h Holistic, w MatchWeights) int {
	x := w.Intercept + w.RoleMatch*h.RoleMatch + w.ExperienceShort*h.ExperienceShort
	return int(math.Round(min(100, max(0, x*(1-h.Blocker)))))
}

// MatchSchemaVersion is the version of the score output contract.
const MatchSchemaVersion = "1"

// MatchResult is the score output (match schema v1): the Match Score, the
// Holistic Round answers behind it, and the tuning file that produced it.
type MatchResult struct {
	SchemaVersion string   `json:"schema_version"`
	Model         string   `json:"model"`
	Tuning        string   `json:"tuning,omitempty"`
	MatchScore    int      `json:"match_score"`
	Signals       Holistic `json:"signals"`
}

// NewMatchResult builds the score output for a Holistic Round answer;
// tuningHash identifies the tuning file.
func NewMatchResult(h Holistic, w MatchWeights, tuningHash string) MatchResult {
	signals := h
	signals.Model = ""
	return MatchResult{SchemaVersion: MatchSchemaVersion, Model: h.Model, Tuning: tuningHash, MatchScore: MatchScore(h, w), Signals: signals}
}
