package eval

import (
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/app"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

func checkerFixture() CheckerFixture {
	job := Fixture{
		ID: "job1",
		JD: domain.JobDescription{Title: "Go Engineer", Text: "Go Engineer\nYou know Golang well.\nKubernetes or Nomad.\n5+ years.\n"},
		Expected: Expected{
			Requirements: []ExpectedRequirement{
				{Value: "Go", Aliases: []string{"Golang"}, Tier: "required"},
				{Value: "Kubernetes", Tier: "preferred"},
				{Value: "Nomad", Tier: "preferred"},
				{Value: "5+ years", Tier: "required"},
				{Value: "Terraform", Tier: "mentioned"},
			},
			AlternativeGroups: [][]string{{"Kubernetes", "Nomad"}},
		},
	}
	resume := "# Experience\n## Eng | Acme | 2020\n- Built Go services on EKS\n- Wrote Terraform modules\n# Skills\n- Go, Nomad\n"
	return CheckerFixture{
		ID: "pair1", Job: job, Resume: domain.Resume{Text: resume}, Units: app.ParseResume(resume),
		Expected: CheckerExpected{
			Job: "job1", Resume: "syn", Skip: []string{"5+ years"},
			Links: map[string][]EvidenceLabel{
				"Go":         {{"Built Go services", "strong"}, {"Go, Nomad", "weak"}},
				"Kubernetes": {{"Built Go services", "strong"}},
				"Nomad":      {{"Go, Nomad", "weak"}},
			},
		},
	}
}

func TestGoldenRequirements(t *testing.T) {
	res := GoldenRequirements(checkerFixture().Job)
	if len(res.Requirements) != 5 || res.Requirements[0].ID != "req_1" || res.Requirements[0].Tier != domain.TierRequired {
		t.Fatalf("requirements = %+v", res.Requirements)
	}
	// Found through the alias, case-insensitively.
	goReq := res.Requirements[0]
	if len(goReq.Refs) != 1 || res.Context[goReq.Refs[0]].Text != "You know Golang well." {
		t.Errorf("Go refs = %v, context = %v", goReq.Refs, res.Context)
	}
	if tf := res.Requirements[4]; len(tf.Refs) != 0 {
		t.Errorf("Terraform refs = %v, want none (not in the text)", tf.Refs)
	}
	if len(res.AlternativeGroups) != 1 || !slices.Equal(res.AlternativeGroups[0].Members, []string{"req_2", "req_3"}) {
		t.Errorf("groups = %+v", res.AlternativeGroups)
	}
}

func TestContainsWord(t *testing.T) {
	tests := []struct {
		text, name string
		want       bool
	}{
		{"You know Golang well.", "golang", true},
		{"Experience at Google.", "Go", false},
		{"Go, Ruby, or C++.", "C++", true},
		{"Use Go.", "go", true},
		{"Gopher Go", "Go", true},
	}
	for _, tt := range tests {
		if got := containsWord(tt.text, tt.name); got != tt.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tt.text, tt.name, got, tt.want)
		}
	}
}

func TestScoreCheck(t *testing.T) {
	f := checkerFixture()
	// e1 = Built Go services, e2 = Wrote Terraform modules, e3 = Go, Nomad.
	link := func(unit string, s domain.EvidenceStrength) domain.EvidenceLink {
		return domain.EvidenceLink{Unit: unit, Strength: s, P: 0.9}
	}
	res := domain.CoverageResult{Requirements: []domain.RequirementCoverage{
		{Value: "Go", Tier: "required", Coverage: "strong", Evidence: []domain.EvidenceLink{link("e1", "strong")}},
		{Value: "Kubernetes", Tier: "preferred", Coverage: "partial", Evidence: []domain.EvidenceLink{link("e1", "partial")}},
		{Value: "Nomad", Tier: "preferred", Coverage: "none", Evidence: []domain.EvidenceLink{}},
		{Value: "5+ years", Tier: "required", Coverage: "weak", Evidence: []domain.EvidenceLink{link("e1", "weak")}},
		{Value: "Terraform", Tier: "mentioned", Coverage: "partial", Evidence: []domain.EvidenceLink{link("e1", "partial")}},
	}}
	trace := domain.CheckTrace{Units: []domain.TraceUnit{
		{ID: "e1", Retrieved: []string{"Go", "Kubernetes", "Terraform", "5+ years"}},
		{ID: "e2", Retrieved: []string{}},
		{ID: "e3", Retrieved: []string{"Go"}},
	}}

	s, err := ScoreCheck(f, res, trace, tuning.Default().FitWeights())
	if err != nil {
		t.Fatal(err)
	}
	// Labeled pairs (skip excluded): Go/e1, Go/e3, Kubernetes/e1, Nomad/e3.
	// Retrieved: Go/e1, Go/e3, Kubernetes/e1.
	if s.LabeledPairs != 4 || s.Retrieved != 3 || s.LabeledStrongPartial != 2 || s.RetrievedStrongPartial != 2 {
		t.Errorf("retrieval = %d/%d (sp %d/%d)", s.Retrieved, s.LabeledPairs, s.RetrievedStrongPartial, s.LabeledStrongPartial)
	}
	// Predicted links: Go/e1, Kubernetes/e1, Terraform/e1 (5+ years skipped).
	if s.PredictedLinks != 3 || s.CorrectLinks != 2 {
		t.Errorf("links = %d correct of %d predicted", s.CorrectLinks, s.PredictedLinks)
	}
	if s.StrengthExact != 1 || s.StrengthNear != 2 {
		t.Errorf("strength exact %d near %d, want 1 2", s.StrengthExact, s.StrengthNear)
	}
	if len(s.MissedLinks) != 2 || len(s.FalseLinks) != 1 || s.FalseLinks[0].Requirement != "Terraform" {
		t.Errorf("missed %+v false %+v", s.MissedLinks, s.FalseLinks)
	}
	// Coverage: Go strong ok; Kubernetes want strong got partial; Nomad want
	// weak got none; Terraform want none got partial.
	if c := s.Coverage["required"]; c != (CoverageTally{Total: 1, Exact: 1, Covered: 1}) {
		t.Errorf("required = %+v", c)
	}
	if c := s.Coverage["preferred"]; c != (CoverageTally{Total: 2, Exact: 0, Covered: 1}) {
		t.Errorf("preferred = %+v", c)
	}
	if c := s.Coverage["mentioned"]; c != (CoverageTally{Total: 1, Exact: 0, Covered: 0}) {
		t.Errorf("mentioned = %+v", c)
	}
	if s.Confusion["strong"]["partial"] != 1 || s.Confusion["weak"]["none"] != 1 || s.Confusion["none"]["partial"] != 1 {
		t.Errorf("confusion = %v", s.Confusion)
	}
	if len(s.CoverageErrors) != 3 {
		t.Errorf("coverage errors = %+v", s.CoverageErrors)
	}
	// Fit over Go, Kubernetes, Nomad, Terraform (weights 3, 1.5, 1.5, 1):
	// predicted 3 + 0.9 + 0 + 0.6 = 4.5 of 7; labeled 3 + 1.5 + 0.45 + 0 = 4.95.
	if s.FitGot == nil || *s.FitGot != 64 || s.FitWant == nil || *s.FitWant != 71 {
		t.Errorf("fit got %v want %v, want 64 and 71", s.FitGot, s.FitWant)
	}
}
