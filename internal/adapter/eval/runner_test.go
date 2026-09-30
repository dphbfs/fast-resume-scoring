package eval

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
)

type fakeExtractor map[string]domain.Result

func (f fakeExtractor) Extract(_ context.Context, jd domain.JobDescription) (domain.Result, error) {
	res, ok := f[jd.Title]
	if !ok {
		return domain.Result{}, errors.New("boom")
	}
	return res, nil
}

func TestRunAndWriteReport(t *testing.T) {
	fixtures := []Fixture{
		{ID: "a", JD: domain.JobDescription{Title: "A"}, Expected: Expected{Requirements: []ExpectedRequirement{
			{Value: "Go", Tier: "required"}, {Value: "Kafka", Tier: "preferred"},
		}}},
		{ID: "b", JD: domain.JobDescription{Title: "B"}, Expected: Expected{Requirements: []ExpectedRequirement{
			{Value: "Rust", Tier: "required"},
		}}},
	}
	ex := fakeExtractor{"A": {Model: "jev-test", Requirements: []domain.Requirement{
		{ID: "r1", Value: "Go"}, {ID: "r2", Value: "Terraform"},
	}}}
	rep := NewRunner(ex, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{MinRequirementMass: 0.5}).
		Run(context.Background(), fixtures, 2)

	tot := rep.Totals
	if tot.Failed != 1 || tot.Expected != 2 || tot.Predicted != 2 || tot.RecallLoose != 0.5 || tot.PrecisionLoose != 0.5 {
		t.Errorf("totals = %+v", tot)
	}
	if rep.Model != "jev-test" || rep.Fixtures[1].Error == "" {
		t.Errorf("report = %+v", rep)
	}

	path, err := rep.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(path)
	for _, want := range []string{"min requirement mass 0.50", "Recall (loose) | 50.0%", "error: boom", "**Missed (1):** `Kafka`", "**Extra (1):** `Terraform`"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if _, err := os.Stat(strings.TrimSuffix(path, ".md") + ".json"); err != nil {
		t.Errorf("json report not written: %v", err)
	}
}

func TestLoadGolden(t *testing.T) {
	fs, err := LoadGolden("../../../testdata/golden", []string{"01a0eeca"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].JD.Title == "" || len(fs[0].Expected.Requirements) == 0 {
		t.Fatalf("fixtures = %+v", fs)
	}
}

func TestRescore(t *testing.T) {
	fixtures := []Fixture{{ID: "a", JD: domain.JobDescription{Title: "A"}, Expected: Expected{
		Requirements: []ExpectedRequirement{{Value: "Go", Tier: "required"}, {Value: "Kafka", Tier: "required"}},
	}}}
	// A report without stored results: rebuilt from matches and extras.
	prev := Report{Fixtures: []FixtureScore{{
		ID: "a", Matches: []Match{{Expected: "Golang", Predicted: "Go"}}, Extras: []string{"Kafka"},
	}}}
	rep := Rescore(prev, fixtures)
	if rep.Totals.RecallLoose != 1 || rep.Totals.PrecisionLoose != 1 || rep.RescoredFrom == "" {
		t.Errorf("rescored totals = %+v", rep.Totals)
	}
}
