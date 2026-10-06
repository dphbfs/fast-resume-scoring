package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-tailoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

func newTestExtractor(t *testing.T, srvURL string, pipeline config.Pipeline) (*Extractor, *metrics.Recorder) {
	t.Helper()
	m := metrics.NewRecorder()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := jev.New(config.Jev{
		APIKey: "k", BaseURL: srvURL, Model: "jev-latest",
		MaxConcurrency: 4, MaxRetries: 0, Timeout: 5 * time.Second,
	}, m, log)
	if err != nil {
		t.Fatal(err)
	}
	return New(c, nil, m, log, pipeline, tuning.Default()), m
}

// sectionFor labels sentences by keyword, standing in for Jev.
func sectionFor(text string) string {
	switch {
	case strings.Contains(text, "years"):
		return "required"
	case strings.Contains(text, "Bonus"):
		return "preferred"
	case strings.Contains(text, "401(k)"):
		return "benefits"
	case strings.Contains(text, "We are"):
		return "company"
	default:
		return "other"
	}
}

// questionSentence returns the sentence embedded in a Section question.
func questionSentence(t *testing.T, q jev.WireQuestion) string {
	t.Helper()
	inst, _ := q.Instructions.(map[string]any)
	s, ok := inst["sentence"].(string)
	if !ok {
		t.Fatalf("question has no embedded sentence: %+v", q.Instructions)
	}
	return s
}

func TestLabelSections(t *testing.T) {
	sentences := []string{
		"Backend Engineer",
		"We are a fintech startup.",
		"5+ years of Go.",
		"Bonus: Kubernetes.",
		"401(k) match.",
	}
	srv := jevtest.NewServer(t, func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			sec := sectionFor(questionSentence(t, q))
			answers[id] = jevtest.Choice(map[string]float64{sec: 0.9, "other": 0.1}, 0.85)
		}
		return jevtest.Reply{Answers: answers}
	})
	e, m := newTestExtractor(t, srv.URL, config.Pipeline{SectionBatchSize: 2})

	r := &run{jd: domain.JobDescription{Title: "Backend Engineer", Text: strings.Join(sentences, "\n")}}
	if err := e.splitSentencesStage(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := e.labelSections(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	want := []domain.Section{"other", "company", "required", "preferred", "benefits"}
	for i, s := range r.sentences {
		if s.Section != want[i] {
			t.Errorf("sentence %d (%q) section = %q, want %q", i, s.Text, s.Section, want[i])
		}
	}
	if r.model != jevtest.Model {
		t.Errorf("model = %q, want %q", r.model, jevtest.Model)
	}

	// 5 sentences in batches of 2 -> 3 requests, each with the full state.
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	for _, req := range reqs {
		if len(req.Questions) > 2 {
			t.Errorf("request has %d questions, want <= 2", len(req.Questions))
		}
		state, _ := req.State.(map[string]any)
		if state["job_title"] != "Backend Engineer" || !strings.Contains(state["job_posting"].(string), "5+ years of Go.") {
			t.Errorf("state = %v, want job title and full posting", state)
		}
		for id, q := range req.Questions {
			crit, _ := q.Criteria.(map[string]any)
			if q.Type != "choice" || len(crit) != len(domain.Sections) {
				t.Errorf("question %s: type %q with %d options, want choice with %d", id, q.Type, len(crit), len(domain.Sections))
			}
		}
	}

	c := m.Summary().Counters
	if c["sections.required"] != 1 || c["sections.dropped"] != 3 {
		t.Errorf("counters = %v, want sections.required=1 sections.dropped=3", c)
	}
}

func TestLabelSectionsRejectsUnknownChoice(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.AnswerAll(func(string, jev.WireQuestion) jev.WireAnswer {
		return jevtest.Choice(map[string]float64{"salary": 1}, 1)
	}))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{SectionBatchSize: 10})

	r := &run{}
	r.sentences, r.headings = splitSentences("Go.")
	if err := e.labelSections(context.Background(), r); err == nil {
		t.Fatal("want error for unknown section, got nil")
	}
}
