package domain

import (
	"maps"
	"slices"
	"testing"
)

func cov(id string, tier Tier, s EvidenceStrength) RequirementCoverage {
	return RequirementCoverage{ID: id, Tier: tier, Coverage: s}
}

func TestScoreFit(t *testing.T) {
	adrExample := []RequirementCoverage{
		cov("r1", TierRequired, StrengthStrong),
		cov("r2", TierRequired, StrengthStrong),
		cov("r3", TierRequired, StrengthPartial),
		cov("r4", TierRequired, StrengthWeak),
		cov("r5", TierRequired, StrengthNone),
		cov("p1", TierPreferred, StrengthStrong),
		cov("p2", TierPreferred, StrengthNone),
		cov("m1", TierMentioned, StrengthPartial),
	}
	tests := []struct {
		name   string
		reqs   []RequirementCoverage
		groups []GroupCoverage
		score  int
		byTier map[Tier]int
		gaps   []string
	}{
		{
			name:   "ADR example",
			reqs:   adrExample,
			score:  57, // 10.8 / 19
			byTier: map[Tier]int{TierRequired: 58, TierPreferred: 50, TierMentioned: 60},
			gaps:   []string{"r5"},
		},
		{
			name: "group takes best member coverage and strongest tier",
			reqs: []RequirementCoverage{
				cov("go", TierPreferred, StrengthNone),
				cov("ruby", TierRequired, StrengthWeak),
				cov("k8s", TierMentioned, StrengthStrong),
			},
			groups: []GroupCoverage{{ID: "alt_1", Members: []string{"go", "ruby"}, Coverage: StrengthStrong}},
			score:  48, // (3*0.3 + 1*1) / 4; the stored group Coverage is ignored
			byTier: map[Tier]int{TierRequired: 30, TierMentioned: 100},
			gaps:   []string{},
		},
		{
			name: "uncovered required group is one gap, by group ID",
			reqs: []RequirementCoverage{
				cov("go", TierRequired, StrengthNone),
				cov("ruby", TierRequired, StrengthNone),
			},
			groups: []GroupCoverage{{ID: "alt_1", Members: []string{"go", "ruby"}}},
			score:  0,
			byTier: map[Tier]int{TierRequired: 0},
			gaps:   []string{"alt_1"},
		},
		{
			name:   "group members missing from reqs are ignored",
			reqs:   []RequirementCoverage{cov("go", TierRequired, StrengthStrong)},
			groups: []GroupCoverage{{ID: "alt_1", Members: []string{"skipped"}}},
			score:  100,
			byTier: map[Tier]int{TierRequired: 100},
			gaps:   []string{},
		},
		{
			name:   "missing tier counts as mentioned",
			reqs:   []RequirementCoverage{cov("a", "", StrengthPartial), cov("b", TierRequired, StrengthStrong)},
			score:  90, // (1*0.6 + 3*1) / 4
			byTier: map[Tier]int{TierMentioned: 60, TierRequired: 100},
			gaps:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreFit(tt.reqs, tt.groups, testFitWeights)
			if got.Score == nil || *got.Score != tt.score {
				t.Errorf("Score = %v, want %d", got.Score, tt.score)
			}
			if !maps.Equal(got.ByTier, tt.byTier) {
				t.Errorf("ByTier = %v, want %v", got.ByTier, tt.byTier)
			}
			if !slices.Equal(got.Gaps, tt.gaps) {
				t.Errorf("Gaps = %v, want %v", got.Gaps, tt.gaps)
			}
		})
	}
}

func TestScoreFitEmpty(t *testing.T) {
	got := ScoreFit(nil, []GroupCoverage{{ID: "alt_1", Members: []string{"x"}}}, testFitWeights)
	if got.Score != nil || len(got.ByTier) != 0 || got.Gaps == nil || len(got.Gaps) != 0 {
		t.Errorf("ScoreFit(empty) = %+v, want null score, empty tiers and gaps", got)
	}
}

// testFitWeights are the shipped Fit Score weights (tuning/tuning.yaml).
var testFitWeights = FitWeights{
	Credit:     map[EvidenceStrength]float64{StrengthStrong: 1, StrengthPartial: 0.6, StrengthWeak: 0.3},
	TierWeight: map[Tier]float64{TierRequired: 3, TierPreferred: 1.5, TierMentioned: 1},
}
