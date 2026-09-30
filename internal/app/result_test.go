package app

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

func TestBuildResult(t *testing.T) {
	e := New(nil, nil, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{})
	r := &run{
		model: "typesafe/jev-1.13-20260917",
		sentences: []domain.ContextSentence{
			{Ref: "s1", Text: "Go and Kafka.", Section: domain.SectionRequired},
			{Ref: "s2", Text: "Dental.", Section: domain.SectionBenefits},
			{Ref: "s3", Text: "More go.", Section: domain.SectionPreferred},
		},
		accepted: []judged{
			{Candidate: domain.Candidate{Text: "Go", Ref: "s1"}},
			{Candidate: domain.Candidate{Text: "Kafka", Ref: "s1"}},
			{Candidate: domain.Candidate{Text: "go", Ref: "s3"}},
		},
		importance: map[string]float64{"kafka": 0.9, "go": 0.4},
	}
	if err := e.buildResult(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	res := r.result

	if res.SchemaVersion != domain.SchemaVersion || res.Model != "typesafe/jev-1.13-20260917" {
		t.Errorf("header = %q %q", res.SchemaVersion, res.Model)
	}
	// Case-insensitive duplicates merge; sorted by importance descending.
	if len(res.Requirements) != 2 {
		t.Fatalf("requirements = %+v, want 2", res.Requirements)
	}
	kafka, goReq := res.Requirements[0], res.Requirements[1]
	if kafka.Value != "Kafka" || kafka.Importance != 0.9 || kafka.ID != "req_1" {
		t.Errorf("first = %+v, want Kafka 0.9 req_1", kafka)
	}
	if goReq.Value != "Go" || !slices.Equal(goReq.Refs, []domain.Ref{"s1", "s3"}) || goReq.ID != "req_2" {
		t.Errorf("second = %+v, want Go refs [s1 s3] req_2", goReq)
	}
	// Context holds only referenced sentences.
	if _, ok := res.Context["s2"]; ok || len(res.Context) != 2 {
		t.Errorf("context = %v, want s1 and s3 only", res.Context)
	}
	if res.AlternativeGroups == nil {
		t.Error("alternative_groups must be an empty list, not null")
	}
}
