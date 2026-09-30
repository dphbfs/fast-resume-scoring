package eval

import (
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
)

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"Master’s  Degree":    "master's degree",
		"  Node.js, C++ (C#)": "node.js c++ c#",
		"CI/CD—pipelines":     "ci/cd pipelines",
	}
	for in, want := range tests {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooseMatch(t *testing.T) {
	tests := []struct {
		expected  []string
		predicted string
		want      bool
	}{
		{[]string{"Kafka"}, "experience with Kafka", true}, // padded
		{[]string{"distributed systems"}, "large-scale distributed systems", true},
		{[]string{"5+ years software engineering"}, "5+ years of software engineering experience", true},
		{[]string{"financial systems"}, "systems", false}, // cut off, one token
		{[]string{"Go"}, "Golang", false},                 // no alias
		{[]string{"Go", "Golang"}, "Golang", true},        // alias
		{[]string{"API design"}, "database design", false},
		{[]string{"streaming data infrastructure"}, "streaming data", true}, // slightly shorter
		// word forms
		{[]string{"alerting"}, "alerts", true},
		{[]string{"mentoring"}, "Mentor engineers", true},
		{[]string{"mentoring"}, "mentorship", true},
		{[]string{"threat modelling"}, "threat models", true},
		{[]string{"certificate lifecycle management"}, "manage certificate lifecycles", true},
		{[]string{"productionizing AI features"}, "productionize AI features", true},
		// slash- and hyphen-joined predictions
		{[]string{"Terraform"}, "terraform/terragrunt", true},
		{[]string{"GitOps"}, "GitOps-style deployment workflows", true},
		{[]string{"manufacturing"}, "manufacturing-adjacent environments", true},
		// stems must not over-merge
		{[]string{"database"}, "data", false},
		{[]string{"servers"}, "services", false},
	}
	for _, tt := range tests {
		if got := looseMatch(tt.expected, tt.predicted); got != tt.want {
			t.Errorf("looseMatch(%q, %q) = %v, want %v", tt.expected, tt.predicted, got, tt.want)
		}
	}
}

func req(id, value string, importance float64) domain.Requirement {
	return domain.Requirement{ID: id, Value: value, Importance: importance}
}

func TestScore(t *testing.T) {
	exp := Expected{
		Requirements: []ExpectedRequirement{
			{Value: "Go", Aliases: []string{"Golang"}, Tier: "required"},
			{Value: "Kafka", Tier: "required"},
			{Value: "distributed systems", Tier: "required"},
			{Value: "Kubernetes", Tier: "preferred"},
			{Value: "mentoring", Tier: "mentioned"},
		},
		AlternativeGroups: [][]string{{"Go", "Kafka"}},
		Filler:            []string{"team player"},
	}
	res := domain.Result{
		Requirements: []domain.Requirement{
			req("r1", "golang", 0.9),                          // strict via alias
			req("r2", "experience with Kafka", 0.8),           // loose
			req("r3", "large-scale distributed systems", 0.7), // loose
			req("r4", "Kubernetes", 0.9),                      // strict, but outranks required ones
			req("r5", "team player", 0.1),                     // filler
			req("r6", "Terraform", 0.2),                       // extra
		},
		AlternativeGroups: []domain.AlternativeGroup{{ID: "a1", Members: []string{"r1", "r2"}}},
	}

	s := Score(exp, res)

	if s.Expected != 5 || s.Predicted != 6 || s.StrictMatched != 2 || s.LooseMatched != 4 {
		t.Errorf("counts = exp %d pred %d strict %d loose %d, want 5 6 2 4", s.Expected, s.Predicted, s.StrictMatched, s.LooseMatched)
	}
	if got := s.Tiers["required"]; got.Expected != 3 || got.Matched != 3 {
		t.Errorf("required tier = %+v, want 3/3", got)
	}
	if got := s.Tiers["mentioned"]; got.Expected != 1 || got.Matched != 0 {
		t.Errorf("mentioned tier = %+v, want 0/1", got)
	}
	if !slices.Equal(s.Misses, []string{"mentoring"}) {
		t.Errorf("misses = %q, want [mentoring]", s.Misses)
	}
	if !slices.Equal(s.FillerHits, []string{"team player"}) {
		t.Errorf("filler hits = %q", s.FillerHits)
	}
	if !slices.Equal(s.Extras, []string{"Terraform"}) {
		t.Errorf("extras = %q, want [Terraform]", s.Extras)
	}

	// Tier order: pairs (required, preferred) = Go/Kubernetes 0.9=0.9 -> 0.5,
	// Kafka 0.8<0.9 -> 0, distributed 0.7<0.9 -> 0. Mean = 0.5/3.
	if s.TierOrder == nil || *s.TierOrder < 0.16 || *s.TierOrder > 0.17 {
		t.Errorf("tier order = %v, want ~0.167", s.TierOrder)
	}
	// Groups: expected pair {Go,Kafka}; predicted pair {Go,Kafka} -> F1 1.
	if s.GroupF1 == nil || *s.GroupF1 != 1 {
		t.Errorf("group F1 = %v, want 1", s.GroupF1)
	}
}

func TestScoreAcceptable(t *testing.T) {
	exp := Expected{
		Requirements: []ExpectedRequirement{{Value: "Go", Tier: "required"}},
		Acceptable:   []string{"repair drift across stack templates"},
		Filler:       []string{"team player"},
	}
	res := domain.Result{Requirements: []domain.Requirement{
		req("r1", "Go", 0), req("r2", "repair drift across stack templates", 0),
		req("r3", "team player", 0), req("r4", "infrastructure", 0),
	}}
	s := Score(exp, res)
	if !slices.Equal(s.AcceptableHits, []string{"repair drift across stack templates"}) {
		t.Errorf("acceptable hits = %q", s.AcceptableHits)
	}
	if !slices.Equal(s.Extras, []string{"infrastructure"}) || !slices.Equal(s.FillerHits, []string{"team player"}) {
		t.Errorf("extras = %q, filler = %q", s.Extras, s.FillerHits)
	}
	// Precision ignores acceptable hits: 1 match / (4 predicted - 1 acceptable).
	if got := s.Precision(); got < 0.333 || got > 0.334 {
		t.Errorf("precision = %v, want 1/3", got)
	}
}

func TestScoreDuplicates(t *testing.T) {
	exp := Expected{Requirements: []ExpectedRequirement{{Value: "Go", Aliases: []string{"Golang"}, Tier: "required"}}}
	res := domain.Result{Requirements: []domain.Requirement{req("r1", "Go", 0), req("r2", "Golang", 0), req("r3", "Rust", 0)}}
	s := Score(exp, res)
	if !slices.Equal(s.DuplicateHits, []string{"Golang"}) || !slices.Equal(s.Extras, []string{"Rust"}) {
		t.Errorf("duplicates = %q, extras = %q", s.DuplicateHits, s.Extras)
	}
	// Precision = 1 match / (3 predicted - 1 duplicate).
	if got := s.Precision(); got != 0.5 {
		t.Errorf("precision = %v, want 0.5", got)
	}
}

func TestScoreNotApplicableWithoutRefinement(t *testing.T) {
	exp := Expected{
		Requirements:      []ExpectedRequirement{{Value: "Go", Tier: "required"}, {Value: "Kafka", Tier: "preferred"}},
		AlternativeGroups: [][]string{{"Go", "Kafka"}},
	}
	res := domain.Result{Requirements: []domain.Requirement{req("r1", "Go", 0), req("r2", "Kafka", 0)}}
	s := Score(exp, res)
	if s.TierOrder != nil {
		t.Errorf("tier order = %v, want n/a when every Importance is equal", *s.TierOrder)
	}
	if s.GroupF1 != nil {
		t.Errorf("group F1 = %v, want n/a when the result has no groups", *s.GroupF1)
	}
}
