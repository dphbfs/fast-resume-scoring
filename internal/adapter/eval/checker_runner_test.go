package eval

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/app"
	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

// echoChecker links every unit that names a Requirement as strong.
type echoChecker struct{}

func (echoChecker) Check(_ context.Context, reqs domain.Result, resume domain.Resume) (domain.CoverageResult, domain.CheckTrace, error) {
	units := app.ParseResume(resume.Text)
	res := domain.CoverageResult{SchemaVersion: domain.CoverageSchemaVersion, Model: "fake"}
	var tr domain.CheckTrace
	for _, u := range units {
		tu := domain.TraceUnit{ID: u.ID, Text: u.Text}
		for _, r := range reqs.Requirements {
			if containsWord(u.Text, r.Value) {
				tu.Retrieved = append(tu.Retrieved, r.Value)
			}
		}
		tr.Units = append(tr.Units, tu)
	}
	for _, r := range reqs.Requirements {
		rc := domain.RequirementCoverage{ID: r.ID, Value: r.Value, Tier: r.Tier, Coverage: domain.StrengthNone}
		for _, u := range units {
			if containsWord(u.Text, r.Value) {
				rc.Evidence = append(rc.Evidence, domain.EvidenceLink{Unit: u.ID, Strength: domain.StrengthStrong, P: 1})
				rc.Coverage = domain.StrengthStrong
			}
		}
		res.Requirements = append(res.Requirements, rc)
	}
	return res, tr, nil
}

func TestCheckerRunWriteAndRescore(t *testing.T) {
	f := checkerFixture()
	r := NewCheckerRunner(echoChecker{}, nil, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Checker{RetrievalK: 5}, tuning.Default())
	rep := r.Run(context.Background(), []CheckerFixture{f}, 2)

	tot := rep.Totals
	if tot.Fixtures != 1 || tot.Failed != 0 || rep.Model != "fake" {
		t.Fatalf("totals = %+v", tot)
	}
	// Echo links Go/e1, Go/e3, Nomad/e3, Terraform/e2 as strong. Labeled:
	// Go/e1 strong, Go/e3 weak, Kubernetes/e1 strong, Nomad/e3 weak.
	if tot.PredictedLinks != 4 || tot.CorrectLinks != 3 || tot.LabeledPairs != 4 {
		t.Errorf("links = %d/%d of %d", tot.CorrectLinks, tot.PredictedLinks, tot.LabeledPairs)
	}
	if tot.RetrievalRecall != 0.75 {
		t.Errorf("retrieval recall = %v, want 0.75 (Kubernetes/e1 not retrieved)", tot.RetrievalRecall)
	}

	dir := t.TempDir()
	md, err := rep.Write(dir)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(md)
	for _, want := range []string{"**Coverage accuracy**", "Coverage confusion", "Missed links (1)", "`Kubernetes` ← e1"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("markdown lacks %q:\n%s", want, text)
		}
	}

	jsonPath := strings.TrimSuffix(md, ".md") + ".json"
	raw, _ := os.ReadFile(jsonPath)
	var prev CheckerReport
	if err := json.Unmarshal(raw, &prev); err != nil {
		t.Fatal(err)
	}
	traces, err := LoadCheckTraces(jsonPath)
	if err != nil || len(traces) != 1 {
		t.Fatalf("traces = %v, %v", traces, err)
	}
	re, err := RescoreChecker(prev, []CheckerFixture{f}, traces, tuning.Default())
	if err != nil {
		t.Fatal(err)
	}
	if re.Totals.CoverageExact != tot.CoverageExact || re.Totals.RetrievalRecall != tot.RetrievalRecall || re.RescoredFrom == "" {
		t.Errorf("rescored totals = %+v, want %+v", re.Totals, tot)
	}
}
