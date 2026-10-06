package eval

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

// titleExtractor returns one Requirement named after the Job Description
// title and counts its calls.
type titleExtractor struct{ calls atomic.Int32 }

func (e *titleExtractor) Extract(_ context.Context, jd domain.JobDescription) (domain.Result, domain.Trace, error) {
	e.calls.Add(1)
	if jd.Title == "boom" {
		return domain.Result{}, domain.Trace{}, errors.New("boom")
	}
	return domain.Result{Requirements: []domain.Requirement{{ID: "req_1", Value: jd.Title, Tier: domain.TierRequired}}}, domain.Trace{}, nil
}

// fixedJudge returns the same Holistic judgment for every pair.
type fixedJudge struct{}

func (fixedJudge) Judge(context.Context, domain.JobDescription, domain.Resume) (domain.Holistic, error) {
	return domain.Holistic{Model: "jev", RoleMatch: 0.5, Blocker: 0.1}, nil
}

// fitChecker returns the Fit Score fits[first Requirement value].
type fitChecker map[string]int

func (c fitChecker) Check(_ context.Context, reqs domain.Result, _ domain.Resume) (domain.CoverageResult, domain.CheckTrace, error) {
	score := c[reqs.Requirements[0].Value]
	return domain.CoverageResult{Model: "fake", Fit: domain.Fit{Score: &score}}, domain.CheckTrace{}, nil
}

func writeReference(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "ref")
	files := map[string]string{
		"resume.md":    "# Skills\n- Go\n",
		"ref/jd/a.txt": "A posting", "ref/jd/b.txt": "B posting", "ref/jd/c.txt": "C posting",
		"ref/pairs.json": `{"pairs": [
			{"id": "a", "title": "Alpha", "resume": "resume.md", "jd": "jd/a.txt", "score": 40, "subset": true},
			{"id": "b", "title": "Beta", "resume": "resume.md", "jd": "jd/b.txt", "score": 60, "subset": true},
			{"id": "c", "title": "Gamma", "resume": "resume.md", "jd": "jd/c.txt", "score": 80, "subset": false}]}`,
		"ref/current.json": `{"pairs": [{"pair": "a", "score": 50}, {"pair": "b", "score": 70}]}`,
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadE2ESets(t *testing.T) {
	dir := writeReference(t)
	root := filepath.Dir(dir)
	for _, tt := range []struct {
		set  string
		want []string
	}{
		{E2ESubset, []string{"a", "b"}},
		{E2ECurrent, []string{"a", "b"}},
		{E2EAll, []string{"a", "b", "c"}},
	} {
		pairs, err := LoadE2E(dir, root, tt.set, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.set, err)
		}
		var got []string
		for _, p := range pairs {
			got = append(got, p.ID)
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%s: pairs = %v, want %v", tt.set, got, tt.want)
		}
	}
	pairs, _ := LoadE2E(dir, root, E2EAll, []string{"c"})
	if len(pairs) != 1 || pairs[0].Reference != nil || pairs[0].Saved == nil || *pairs[0].Saved != 80 || pairs[0].JD.Title != "Gamma" {
		t.Errorf("pair c = %+v", pairs)
	}
	if _, err := LoadE2E(dir, root, "nope", nil); err == nil {
		t.Error("unknown set: want error")
	}
}

func TestLoadE2EWithoutSavedScores(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"resume.md":          "# Skills\n- Go\n",
		"final/jd/a.txt":     "A posting",
		"final/pairs.json":   `{"pairs": [{"id": "a", "title": "Alpha", "resume": "resume.md", "jd": "jd/a.txt"}]}`,
		"final/current.json": `{"pairs": [{"pair": "a", "score": 61}]}`,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := LoadE2E(filepath.Join(root, "final"), root, E2ECurrent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].Saved != nil || pairs[0].Reference == nil || *pairs[0].Reference != 61 {
		t.Errorf("pairs = %+v", pairs)
	}
	fifty := 50
	tot := e2eTotals([]E2EScore{{ID: "a", Reference: pairs[0].Reference, Match: &fifty}}, 0)
	if tot.SavedPairs != 0 || tot.Scored != 1 {
		t.Errorf("totals = %+v", tot)
	}
}

func TestE2ERunScoresAndCachesExtraction(t *testing.T) {
	dir := writeReference(t)
	pairs, err := LoadE2E(dir, filepath.Dir(dir), E2EAll, nil)
	if err != nil {
		t.Fatal(err)
	}
	ex := &titleExtractor{}
	r := NewE2ERunner(ex, fitChecker{"Alpha": 54, "Beta": 58, "Gamma": 90}, fixedJudge{}, metrics.NewRecorder(),
		slog.New(slog.NewTextHandler(io.Discard, nil)), config.Jev{Model: "m"}, config.Pipeline{}, config.Checker{}, tuning.Default())
	cache := filepath.Join(t.TempDir(), "extract")

	rep := r.Run(context.Background(), pairs, 2, cache, true)
	tot := rep.Totals
	// Match Score (80.7 + 18.5*0.5) * 0.9 = 81 for every pair: errors +31
	// (a), +11 (b).
	if tot.Scored != 2 || tot.Failed != 0 || tot.MAE != 21 || tot.Bias != 21 || tot.MaxError != 31 || tot.SavedPairs != 3 {
		t.Errorf("totals = %+v", tot)
	}
	// Fit errors vs reference: +4 (a), -12 (b).
	if f := tot.Fit; f == nil || f.MAE != 8 || f.Bias != -4 || f.Within5 != 0.5 || f.Within10 != 0.5 || tot.ColdPairs != 3 {
		t.Errorf("fit totals = %+v (%+v)", f, tot)
	}

	rep = r.Run(context.Background(), pairs, 2, cache, true)
	if ex.calls.Load() != 3 || rep.Totals.ColdPairs != 0 {
		t.Errorf("second run: %d extractor calls, %d cold pairs; want 3 and 0", ex.calls.Load(), rep.Totals.ColdPairs)
	}
	rep.Set = E2EAll
	md, err := rep.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(md)
	if !strings.Contains(string(raw), "**Match Score MAE vs reference** | **21.0**") {
		t.Errorf("markdown:\n%s", raw)
	}
	if rep.Pairs[0].Holistic == nil || rep.Pairs[0].Holistic.RoleMatch != 0.5 || rep.Model != "jev" {
		t.Errorf("holistic = %+v", rep.Pairs[0].Holistic)
	}
	if _, err := os.Stat(strings.TrimSuffix(md, ".md") + "-traces/a.json"); err != nil {
		t.Errorf("trace not written: %v", err)
	}
}

func TestE2ERunReportsFailedPair(t *testing.T) {
	pairs := []E2EPair{{ID: "x", JD: domain.JobDescription{Title: "boom"}}}
	r := NewE2ERunner(&titleExtractor{}, fitChecker{}, fixedJudge{}, metrics.NewRecorder(),
		slog.New(slog.NewTextHandler(io.Discard, nil)), config.Jev{}, config.Pipeline{}, config.Checker{}, tuning.Default())
	rep := r.Run(context.Background(), pairs, 1, "", true)
	if rep.Totals.Failed != 1 || !strings.Contains(rep.Pairs[0].Error, "extract: boom") {
		t.Errorf("report = %+v", rep.Pairs)
	}
}

func TestE2ERunWithoutFit(t *testing.T) {
	dir := writeReference(t)
	pairs, err := LoadE2E(dir, filepath.Dir(dir), E2EAll, nil)
	if err != nil {
		t.Fatal(err)
	}
	ex := &titleExtractor{}
	r := NewE2ERunner(ex, fitChecker{}, fixedJudge{}, metrics.NewRecorder(),
		slog.New(slog.NewTextHandler(io.Discard, nil)), config.Jev{}, config.Pipeline{}, config.Checker{}, tuning.Default())
	rep := r.Run(context.Background(), pairs, 2, "", false)
	if ex.calls.Load() != 0 || rep.Totals.Fit != nil || rep.Totals.Scored != 2 || rep.Pairs[0].Fit != nil || *rep.Pairs[0].Match != 81 {
		t.Errorf("report = %+v, %d extractor calls", rep.Totals, ex.calls.Load())
	}
}

func TestKendallTauB(t *testing.T) {
	for _, tt := range []struct {
		x, y []float64
		want float64
	}{
		{[]float64{1, 2, 3}, []float64{10, 20, 30}, 1},
		{[]float64{1, 2, 3}, []float64{30, 20, 10}, -1},
		// One tie in x: C=2, D=0, tiesX=1 -> 2/sqrt(3*2).
		{[]float64{1, 1, 2}, []float64{1, 2, 3}, 2 / math.Sqrt(6)},
		{[]float64{5, 5}, []float64{1, 2}, 0},
	} {
		if got := kendallTauB(tt.x, tt.y); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("kendallTauB(%v, %v) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}
