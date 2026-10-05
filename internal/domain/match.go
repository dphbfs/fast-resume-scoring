package domain

import "math"

// Match Score weights (docs/adr/0003), fitted by least squares on the
// 30-pair development subset (testdata/reference) in scoring mode, pooled
// over runs 2026-10-05T12-24-46Z, 12-25-55Z, and 12-27-01Z (blocker asked
// without location). Refit only on development data.
const (
	matchIntercept        = 44.2
	matchFitWeight        = 31.7
	matchResponsibilities = 46.2
	matchDomainMismatch   = -17.5
)

// MatchScore is the overall 0-100 match of a Resume to a posting, on the
// generative reference's scale: the Fit Score (Requirement Coverage) and
// the Holistic Round's responsibilities share, less a domain-mismatch
// penalty, all discounted by the probability of a clearly unmet hard
// eligibility condition other than location. Nil when the Fit Score is nil.
func MatchScore(fit Fit, h Holistic) *int {
	if fit.Score == nil {
		return nil
	}
	x := matchIntercept + matchFitWeight*float64(*fit.Score)/100 +
		matchResponsibilities*h.Responsibilities + matchDomainMismatch*h.DomainMismatch
	s := int(math.Round(min(100, max(0, x*(1-h.Blocker)))))
	return &s
}
