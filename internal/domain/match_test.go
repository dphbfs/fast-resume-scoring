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
		// 42.8 + 48.4*0.5 + 38.9*0.5 - 19.4*0 = 86.45
		{"blend", score(50), Holistic{Responsibilities: 0.5}, ptr(86)},
		// 42.8 + 0 + 0 - 19.4 = 23.4
		{"domain mismatch", score(0), Holistic{DomainMismatch: 1}, ptr(23)},
		{"blocker discounts", score(50), Holistic{Responsibilities: 0.5, Blocker: 0.5}, ptr(43)},
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
