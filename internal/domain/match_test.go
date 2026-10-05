package domain

import "testing"

func TestMatchScore(t *testing.T) {
	score := func(n int) Fit { return Fit{Score: &n} }
	for _, tt := range []struct {
		name string
		fit  Fit
		h    Holistic
		want *int
	}{
		{"blend", score(50), Holistic{CoreWork: 0.5}, ptr(75)}, // 21.2 + 107.9*0.5
		{"blocker discounts", score(50), Holistic{CoreWork: 0.5, Blocker: 1}, ptr(21)},
		{"clamped", score(100), Holistic{CoreWork: 1}, ptr(100)},
		{"no fit score", Fit{}, Holistic{CoreWork: 1}, nil},
	} {
		got := MatchScore(tt.fit, tt.h)
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%s: MatchScore = %v, want %v", tt.name, deref(got), deref(tt.want))
		}
	}
}

func ptr(n int) *int { return &n }

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
