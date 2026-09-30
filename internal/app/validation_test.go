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

// questionChunk returns the chunk embedded in a Validation question.
func questionChunk(t *testing.T, q jev.WireQuestion) string {
	t.Helper()
	inst, _ := q.Instructions.(map[string]any)
	c, ok := inst["chunk"].(string)
	if !ok {
		t.Fatalf("question has no embedded chunk: %+v", q.Instructions)
	}
	return c
}

// selectFor answers each chunk question with the option pick[chunk] (0.8),
// or the generic_trait rejection when the chunk is not listed.
func selectFor(t *testing.T, pick map[string]string) jevtest.Responder {
	return func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			crit := q.Criteria.(map[string]any)
			for opt := range rejectOptions {
				if _, ok := crit[opt]; !ok {
					t.Errorf("chunk %q: rejection option %q missing", questionChunk(t, q), opt)
				}
			}
			choice, ok := pick[questionChunk(t, q)]
			if !ok {
				choice = "generic_trait"
			}
			if _, valid := crit[choice]; !valid {
				t.Errorf("chunk %q: %q is not an option", questionChunk(t, q), choice)
			}
			answers[id] = jevtest.Choice(map[string]float64{choice: 0.8, "action_only": 0.2}, 0.7)
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

func TestValidationRoundSelectsOneSpanPerChunk(t *testing.T) {
	srv := jevtest.NewServer(t, selectFor(t, map[string]string{
		"streaming data infrastructure": "streaming data infrastructure",
		"Kafka":                         "Kafka",
		"5+ years of backend work":      "5+ years of backend work",
	}))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})

	r := &run{
		summary: "A backend role.",
		sentences: []domain.ContextSentence{
			{Ref: "s1", Text: "Work closely with streaming data infrastructure such as Kafka", Section: domain.SectionRequired},
			{Ref: "s2", Text: "Dental", Section: domain.SectionBenefits},
			{Ref: "s3", Text: "5+ years of backend work", Section: domain.SectionRequired},
		},
	}
	r.chunks = append(chunkSentence(r.sentences[0], 4), chunkSentence(r.sentences[2], 4)...)

	if err := e.validationRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	want := []string{"streaming data infrastructure", "Kafka", "5+ years of backend work"}
	if got := acceptedTexts(r.accepted); !slices.Equal(got, want) {
		t.Errorf("accepted = %q, want %q", got, want)
	}
	if r.accepted[0].Ref != "s1" || r.accepted[2].Ref != "s3" || r.accepted[0].P != 0.8 {
		t.Errorf("accepted = %+v", r.accepted)
	}

	if len(r.trace.Chunks) != 4 {
		t.Fatalf("trace chunks = %d, want 4", len(r.trace.Chunks))
	}
	for _, tc := range r.trace.Chunks {
		if len(tc.Top) == 0 || len(tc.Candidates) != tc.Options {
			t.Errorf("trace chunk %+v lacks top options or candidates", tc)
		}
		if (tc.Selected == "") == (tc.RejectReason == "") {
			t.Errorf("trace chunk %q: want exactly one of selected/reject reason, got %+v", tc.Text, tc)
		}
	}
	if tc := r.trace.Chunks[0]; tc.Text != "Work closely" || tc.RejectReason != "generic_trait" {
		t.Errorf("first trace chunk = %+v, want Work closely rejected as generic_trait", tc)
	}

	// One request per sentence with chunks (s1, s3); s1 has 3 chunks.
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
			if q.Type != "choice" {
				t.Errorf("question type = %q, want choice", q.Type)
			}
		}
	}

	c := m.Summary().Counters
	if c["validation.accepted"] != 3 || c["validation.rejected.generic_trait"] != 1 {
		t.Errorf("counters = %v, want accepted=3 rejected.generic_trait=1", c)
	}
}

func TestValidationRoundRejectsUnknownOption(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Choice(map[string]float64{"something else": 1}, 1)
	}))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{})
	r := &run{sentences: []domain.ContextSentence{{Ref: "s1", Text: "Kafka", Section: domain.SectionRequired}}}
	r.chunks = chunkSentence(r.sentences[0], 4)
	if err := e.validationRound(context.Background(), r); err == nil {
		t.Fatal("want error for an answer that is not an option")
	}
}

// Rejection wins on summed mass even when a single phrase is the most
// probable option.
func TestValidationRoundRejectsOnSummedMass(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Choice(map[string]float64{
			"Work closely": 0.40, "generic_trait": 0.20, "action_only": 0.25, "people_or_context": 0.15,
		}, 0.3)
	}))
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{})
	r := &run{sentences: []domain.ContextSentence{{Ref: "s1", Text: "Work closely", Section: domain.SectionResponsibilities}}}
	r.chunks = chunkSentence(r.sentences[0], 4)
	if err := e.validationRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(r.accepted) != 0 {
		t.Errorf("accepted = %q, want none (rejection mass 0.60)", acceptedTexts(r.accepted))
	}
	if got := m.Summary().Counters["validation.rejected.action_only"]; got != 1 {
		t.Errorf("rejected.action_only = %d, want 1 (the most probable rejection reason)", got)
	}
}

// When phrases split the mass, their sum still beats a single rejection.
func TestValidationRoundAcceptsOnSummedSpanMass(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Choice(map[string]float64{
			"distributed systems": 0.35, "distributed": 0.20, "systems": 0.10, "generic_trait": 0.35,
		}, 0.3)
	}))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{})
	r := &run{sentences: []domain.ContextSentence{{Ref: "s1", Text: "distributed systems", Section: domain.SectionRequired}}}
	r.chunks = chunkSentence(r.sentences[0], 4)
	if err := e.validationRound(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := acceptedTexts(r.accepted); len(got) != 1 || got[0] != "distributed systems" {
		t.Errorf("accepted = %q, want [distributed systems]", got)
	}
}
