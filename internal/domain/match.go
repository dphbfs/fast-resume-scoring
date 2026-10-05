package domain

import "math"

// Match Score constants (docs/adr/0003). The blend weight and the linear
// map onto the generative reference's 0-100 scale were fitted on the
// 30-pair development subset (testdata/reference) in scoring mode (run
// 2026-10-05T11-40-47Z; weight from 2026-10-05T02-59-15Z); refit them only
// on development data.
const (
	matchFitWeight = 0.5
	matchOffset    = 21.5
	matchScale     = 99.9
)

// MatchScore is the overall 0-100 match of a Resume to a posting: the Fit
// Score (Requirement Coverage) blended with the Holistic core-work share,
// discounted by the probability of a clearly unmet hard eligibility
// condition, then mapped onto the reference scale. Nil when the Fit Score
// is nil.
func MatchScore(fit Fit, h Holistic) *int {
	if fit.Score == nil {
		return nil
	}
	x := (matchFitWeight*float64(*fit.Score)/100 + (1-matchFitWeight)*h.CoreWork) * (1 - h.Blocker)
	s := int(math.Round(min(100, max(0, matchOffset+matchScale*x))))
	return &s
}
