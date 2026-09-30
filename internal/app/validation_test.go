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
// or no_requirement when the chunk is not listed.
func selectFor(t *testing.T, pick map[string]string) jevtest.Responder {
	return func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			choice, ok := pick[questionChunk(t, q)]
			if !ok {
				choice = noRequirement
			}
			if _, valid := q.Criteria.(map[string]any)[choice]; !valid {
				t.Errorf("chunk %q: %q is not an option", questionChunk(t, q), choice)
			}
			answers[id] = jevtest.Choice(map[string]float64{choice: 0.8, noRequirement: 0.2}, 0.7)
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
			{Ref: "s1", Text: "Experience with streaming data infrastructure such as Kafka", Section: domain.SectionRequired},
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
	if c["validation.accepted"] != 3 || c["validation.no_requirement"] != 1 {
		t.Errorf("counters = %v, want accepted=3 no_requirement=1", c)
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
