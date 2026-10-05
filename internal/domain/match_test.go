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
		// 44.2 + 31.7*0.5 + 46.2*0.5 - 17.5*0 = 83.15
		{"blend", score(50), Holistic{Responsibilities: 0.5}, ptr(83)},
		// 44.2 + 0 + 0 - 17.5 = 26.7
		{"domain mismatch", score(0), Holistic{DomainMismatch: 1}, ptr(27)},
		{"blocker discounts", score(50), Holistic{Responsibilities: 0.5, Blocker: 0.5}, ptr(42)},
		{"clamped", score(100), Holistic{Responsibilities: 1}, ptr(100)},
		{"no fit score", Fit{}, Holistic{Responsibilities: 1}, nil},
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
