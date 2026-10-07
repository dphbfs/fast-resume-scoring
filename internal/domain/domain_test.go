package domain

import "testing"

func TestTierFor(t *testing.T) {
	tests := []struct {
		name     string
		sections []Section
		want     Tier
	}{
		{"required wins", []Section{SectionResponsibilities, SectionPreferred, SectionRequired}, TierRequired},
		{"preferred beats responsibilities", []Section{SectionResponsibilities, SectionPreferred}, TierPreferred},
		{"responsibilities only", []Section{SectionResponsibilities}, TierMentioned},
		{"other sections", []Section{SectionOther, SectionCompany}, TierMentioned},
		{"no sections", nil, TierMentioned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TierFor(tt.sections); got != tt.want {
				t.Errorf("TierFor(%v) = %q, want %q", tt.sections, got, tt.want)
			}
		})
	}
}
