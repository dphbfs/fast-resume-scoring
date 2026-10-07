package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev/jevtest"
	"github.com/dphbfs/fast-resume-scoring/internal/domain"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
)

// pipelineResponder answers every question type of the pipeline.
func pipelineResponder(t *testing.T) jevtest.Responder {
	t.Helper()
	return func(_ int, req jev.WireRequest) jevtest.Reply {
		answers := map[string]jev.WireAnswer{}
		for id, q := range req.Questions {
			inst, _ := q.Instructions.(map[string]any)
			crit, _ := q.Criteria.(map[string]any)
			switch {
			case strings.HasPrefix(id, "section_"):
				sec := "required"
				switch s := inst["sentence"].(string); {
				case strings.HasSuffix(s, ":") || s == "Backend Engineer":
					sec = "other"
				case strings.Contains(s, "insurance"):
					sec = "benefits"
				}
				answers[id] = jevtest.Choice(map[string]float64{sec: 0.9, "other": 0.1}, 0.8)
			case strings.HasPrefix(id, "chunk_"):
				pick := inst["chunk"].(string)
				if _, ok := crit[pick]; !ok {
					pick = "generic_trait"
				}
				answers[id] = jevtest.Choice(map[string]float64{pick: 0.9, "action_only": 0.1}, 0.8)
			case strings.HasPrefix(id, "filler_"):
				answers[id] = jevtest.Choice(map[string]float64{fillerKeep: 0.9, "vague_term": 0.1}, 0.8)
			case strings.HasPrefix(id, "dup_"):
				answers[id] = jevtest.Choice(map[string]float64{"different_thing": 1}, 1)
			case strings.HasPrefix(id, "alt_"):
				answers[id] = jevtest.Choice(map[string]float64{"unrelated": 1}, 1)
			case strings.HasPrefix(id, "importance_"):
				answers[id] = jevtest.Score(3, 0.9, map[string]float64{})
			default:
				t.Errorf("unexpected question %q", id)
			}
		}
		return jevtest.Reply{Answers: answers}
	}
}

func TestExtractReturnsResultAndTrace(t *testing.T) {
	srv := jevtest.NewServer(t, pipelineResponder(t))
	e, _ := newTestExtractor(t, srv.URL, config.Pipeline{MaxWindowWords: 4, SectionBatchSize: 60})
	jd := domain.JobDescription{
		Title: "Backend Engineer",
		Text:  "Backend Engineer\nRequirements:\n- 5+ years of Go\n- Kafka\nBenefits:\n- Dental insurance\n",
	}

	res, tr, err := e.Extract(context.Background(), jd)
	if err != nil {
		t.Fatal(err)
	}

	var values []string
	for _, r := range res.Requirements {
		values = append(values, r.Value)
	}
	if !slices.Equal(values, []string{"5+ years of Go", "Kafka"}) || res.Model != jevtest.Model {
		t.Errorf("result = %+v", res)
	}

	if !tr.JobSummary.Fallback {
		t.Errorf("trace summary = %+v, want fallback (no generator)", tr.JobSummary)
	}
	dropped := 0
	for _, s := range tr.Sentences {
		if s.Dropped {
			dropped++
		}
	}
	if len(tr.Sentences) != 6 || dropped != 4 {
		t.Errorf("trace sentences = %d (%d dropped), want 6 (4 dropped)", len(tr.Sentences), dropped)
	}
	if len(tr.Chunks) != 2 || len(tr.Refinement) != 2 || !tr.Refinement[0].Kept {
		t.Errorf("trace chunks = %+v, refinement = %+v", tr.Chunks, tr.Refinement)
	}
}
