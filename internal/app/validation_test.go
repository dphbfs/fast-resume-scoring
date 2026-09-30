package app

import (
	"context"
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
)

// questionCandidate returns the candidate embedded in a Validation question.
func questionCandidate(t *testing.T, q jev.WireQuestion) string {
	t.Helper()
	inst, _ := q.Instructions.(map[string]any)
	c, ok := inst["candidate"].(string)
	if !ok {
		t.Fatalf("question has no embedded candidate: %+v", q.Instructions)
	}
	return c
}

// acceptOnly answers "technology" (0.9) for the listed candidates and
// "generic" (0.9) otherwise.
func acceptOnly(t *testing.T, accept ...string) jevtest.Responder {
	return func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			probs := map[string]float64{"technology": 0.1, "generic": 0.9}
			if slices.Contains(accept, questionCandidate(t, q)) {
				probs = map[string]float64{"technology": 0.9, "generic": 0.1}
			}
			answers[id] = jevtest.Choice(probs, 0.8)
		}
		return jevtest.Reply{Answers: answers}
	}
}

func acceptedTexts(js []judged) []string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = j.Text
	}
	return out
}

func validationRun() *run {
	r := &run{
		summary: "A backend role.",
		sentences: []domain.ContextSentence{
			{Ref: "s1", Text: "5+ years of Go", Section: domain.SectionRequired},
			{Ref: "s2", Text: "Dental", Section: domain.SectionBenefits},
			{Ref: "s3", Text: "Bonus: Kafka", Section: domain.SectionPreferred},
		},
	}
	r.candidates = []domain.Candidate{
		{Ref: "s1", Text: "5+ years"},
		{Ref: "s1", Text: "5+ years of Go"},
		{Ref: "s1", Text: "years"},
		{Ref: "s1", Text: "Go"},
		{Ref: "s3", Text: "Bonus"},
		{Ref: "s3", Text: "Kafka"},
	}
	return r
}

func TestValidationRound(t *testing.T) {
	srv := jevtest.NewServer(t, acceptOnly(t, "5+ years of Go", "Go", "Kafka"))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})

	r := validationRun()
	if err := e.validationRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if got := acceptedTexts(r.accepted); !slices.Equal(got, []string{"5+ years of Go", "Go", "Kafka"}) {
		t.Errorf("accepted = %q", got)
	}
	for _, a := range r.accepted {
		if a.P != 0.9 || a.Kind != "technology" {
			t.Errorf("%q P=%v kind=%q, want 0.9 technology", a.Text, a.P, a.Kind)
		}
	}

	// One request per sentence with candidates (s1, s3), none for s2.
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	for _, req := range reqs {
		state, _ := req.State.(map[string]any)
		if state["job_summary"] != "A backend role." || state["sentence"] == nil || state["section"] == nil {
			t.Errorf("state = %v, want job_summary, section and sentence", state)
		}
		for _, q := range req.Questions {
			crit, _ := q.Criteria.(map[string]any)
			if q.Type != "choice" || len(crit) != len(validationCriteria) {
				t.Errorf("question: type %q with %d options, want choice with %d", q.Type, len(crit), len(validationCriteria))
			}
		}
	}

	c := m.Summary().Counters
	if c["validation.accepted"] != 3 || c["validation.rejected"] != 3 {
		t.Errorf("counters = %v, want accepted=3 rejected=3", c)
	}
}

func TestValidationRoundDropsCompounds(t *testing.T) {
	srv := jevtest.NewServer(t, acceptOnly(t, "Go", "Kubernetes", "Go and Kubernetes", "Kubernetes clusters"))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})

	r := &run{
		sentences: []domain.ContextSentence{{Ref: "s1", Text: "Go and Kubernetes clusters", Section: domain.SectionRequired}},
		candidates: []domain.Candidate{
			{Ref: "s1", Text: "Go"},
			{Ref: "s1", Text: "Go and Kubernetes"},
			{Ref: "s1", Text: "Kubernetes"},
			{Ref: "s1", Text: "Kubernetes clusters"},
		},
	}
	if err := e.validationRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// "Go and Kubernetes" holds two other accepted candidates without
	// overlap, so it is dropped. "Kubernetes clusters" holds only one.
	if got := acceptedTexts(r.accepted); !slices.Equal(got, []string{"Go", "Kubernetes", "Kubernetes clusters"}) {
		t.Errorf("accepted = %q", got)
	}
	if got := m.Summary().Counters["validation.compound_dropped"]; got != 1 {
		t.Errorf("validation.compound_dropped = %d, want 1", got)
	}
}

func TestContainedWords(t *testing.T) {
	tests := []struct {
		outer, inner string
		want         []int
	}{
		{"Go and Kubernetes", "Go", []int{0}},
		{"Go and Kubernetes", "kubernetes", []int{2}},
		{"Golang services", "Go", nil},
		{"C++ and C", "C", []int{2}},
	}
	for _, tt := range tests {
		if got := wordMatches(tt.outer, tt.inner); !slices.Equal(got, tt.want) {
			t.Errorf("wordMatches(%q, %q) = %v, want %v", tt.outer, tt.inner, got, tt.want)
		}
	}
}

func TestSurroundingWords(t *testing.T) {
	tests := []struct {
		sentence, candidate, before, after string
	}{
		{"5+ years building large-scale distributed systems in production", "distributed", "years building large-scale", "systems in production"},
		{"Kafka, Flink, Spark", "Kafka", "", "Flink, Spark"},
		{"Google Go experience", "Go", "Google", "experience"},
		{"Go", "Rust", "", ""},
	}
	for _, tt := range tests {
		b, a := surroundingWords(tt.sentence, tt.candidate, 2)
		if tt.candidate == "distributed" {
			b, a = surroundingWords(tt.sentence, tt.candidate, 3)
		}
		if b != tt.before || a != tt.after {
			t.Errorf("surroundingWords(%q, %q) = (%q, %q), want (%q, %q)", tt.sentence, tt.candidate, b, a, tt.before, tt.after)
		}
	}
}
