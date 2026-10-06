package domain

import (
	"math"
	"strings"
	"testing"
)

func validResult() Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Requirements: []Requirement{
			{ID: "req_1", Value: "Go", Refs: []Ref{"s1"}, Tier: TierRequired, Importance: 0.8},
			{ID: "req_2", Value: "Rust", Refs: []Ref{"s1"}, Tier: TierPreferred},
		},
		AlternativeGroups: []AlternativeGroup{{ID: "alt_1", Members: []string{"req_1", "req_2"}}},
		Context:           map[Ref]ContextSentence{"s1": {Text: "Go or Rust.", Section: SectionRequired}},
	}
}

func TestResultValidate(t *testing.T) {
	if err := validResult().Validate(); err != nil {
		t.Fatalf("valid result: %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*Result)
		want   string
	}{
		{"duplicate id", func(r *Result) { r.Requirements[1].ID = "req_1" }, "duplicate id"},
		{"empty id", func(r *Result) { r.Requirements[0].ID = "" }, "empty id"},
		{"empty value", func(r *Result) { r.Requirements[0].Value = "  " }, "empty value"},
		{"unknown tier", func(r *Result) { r.Requirements[0].Tier = "must" }, `unknown tier "must"`},
		{"missing tier", func(r *Result) { r.Requirements[0].Tier = "" }, `unknown tier ""`},
		{"NaN importance", func(r *Result) { r.Requirements[0].Importance = math.NaN() }, "importance"},
		{"dangling ref", func(r *Result) { r.Requirements[0].Refs = []Ref{"s9"} }, "ref s9 not in context"},
		{"no refs", func(r *Result) { r.Requirements[0].Refs = nil }, "no refs"},
		{"unknown section", func(r *Result) { r.Context["s1"] = ContextSentence{Text: "x", Section: "perks"} }, `unknown section "perks"`},
		{"one-member group", func(r *Result) { r.AlternativeGroups[0].Members = []string{"req_1"} }, "want at least 2"},
		{"unknown member", func(r *Result) { r.AlternativeGroups[0].Members = []string{"req_1", "req_9"} }, "unknown member req_9"},
		{"member in two groups", func(r *Result) {
			r.AlternativeGroups = append(r.AlternativeGroups, AlternativeGroup{ID: "alt_2", Members: []string{"req_1", "req_2"}})
		}, "in groups alt_1 and alt_2"},
	} {
		r := validResult()
		tt.mutate(&r)
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}
