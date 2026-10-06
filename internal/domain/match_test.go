package domain

import "testing"

// testMatchWeights are the shipped weights (tuning/tuning.yaml), so the
// expected scores below read as real Match Scores.
var testMatchWeights = MatchWeights{Intercept: 80.7, RoleMatch: 18.5, ExperienceShort: -55.2}

func TestMatchScore(t *testing.T) {
	for _, tt := range []struct {
		name string
		h    Holistic
		want int
	}{
		// 80.7 + 18.5 = 99.2
		{"same role, qualified", Holistic{RoleMatch: 1}, 99},
		// 80.7 - 55.2 = 25.5
		{"different role, under-qualified", Holistic{ExperienceShort: 1}, 26},
		// (80.7 + 9.25 - 27.6) * 0.5 = 31.2
		{"blocker discounts", Holistic{RoleMatch: 0.5, ExperienceShort: 0.5, Blocker: 0.5}, 31},
		{"blocked", Holistic{RoleMatch: 1, Blocker: 1}, 0},
	} {
		if got := MatchScore(tt.h, testMatchWeights); got != tt.want {
			t.Errorf("%s: MatchScore = %d, want %d", tt.name, got, tt.want)
		}
	}
}
