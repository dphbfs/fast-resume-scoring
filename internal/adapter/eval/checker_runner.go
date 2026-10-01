package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
)

// CheckerRunner runs the Resume Checker over the checker fixtures.
type CheckerRunner struct {
	checker  port.ResumeChecker
	recorder *metrics.Recorder
	log      *slog.Logger
	cfg      config.Checker
}

// NewCheckerRunner builds a CheckerRunner. cfg is recorded in each report.
func NewCheckerRunner(checker port.ResumeChecker, recorder *metrics.Recorder, log *slog.Logger, cfg config.Checker) *CheckerRunner {
	return &CheckerRunner{checker: checker, recorder: recorder, log: log.With("component", "eval"), cfg: cfg}
}

// CheckerReport is one Resume Checker eval run.
type CheckerReport struct {
	Started      time.Time       `json:"started"`
	Duration     string          `json:"duration"`
	Model        string          `json:"model"`
	Revision     string          `json:"revision"`
	Checker      config.Checker  `json:"checker"`
	Labels       string          `json:"labels"`
	RescoredFrom string          `json:"rescored_from,omitempty"`
	Totals       CheckerTotals   `json:"totals"`
	Fixtures     []CheckerScore  `json:"fixtures"`
	Metrics      metrics.Summary `json:"metrics"`
}

// CheckerTotals are micro-averaged over fixtures that ran without error.
type CheckerTotals struct {
	Fixtures int `json:"fixtures"`
	Failed   int `json:"failed"`

	RetrievalRecall              float64 `json:"retrieval_recall"`
	RetrievalRecallStrongPartial float64 `json:"retrieval_recall_strong_partial"`
	LinkPrecision                float64 `json:"link_precision"`
	LinkRecall                   float64 `json:"link_recall"`
	LinkF1                       float64 `json:"link_f1"`
	StrengthExact                float64 `json:"strength_exact"`
	StrengthNear                 float64 `json:"strength_near"`
	// CoverageExact is the main number: Requirements whose Coverage equals
	// the label. CoverageCovered only checks none vs some evidence.
	CoverageExact   float64                   `json:"coverage_exact"`
	CoverageCovered float64                   `json:"coverage_covered"`
	Coverage        map[string]CoverageTally  `json:"coverage"`
	Confusion       map[string]map[string]int `json:"confusion"`

	LabeledPairs   int `json:"labeled_pairs"`
	PredictedLinks int `json:"predicted_links"`
	CorrectLinks   int `json:"correct_links"`
}

// Run checks and scores every fixture, at most parallel at a time. A
// fixture that fails is reported with its error; the run continues.
func (r *CheckerRunner) Run(ctx context.Context, fixtures []CheckerFixture, parallel int) CheckerReport {
	start := time.Now()
	scores := make([]CheckerScore, len(fixtures))
	var mu sync.Mutex
	model := ""

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(1, parallel))
	for i, f := range fixtures {
		g.Go(func() error {
			t0 := time.Now()
			res, tr, err := r.checker.Check(gctx, GoldenRequirements(f.Job), f.Resume)
			var s CheckerScore
			if err == nil {
				s, err = ScoreCheck(f, res, tr)
				s.Result, s.Trace = &res, &tr
				mu.Lock()
				model = res.Model
				mu.Unlock()
			}
			if err != nil {
				r.log.ErrorContext(gctx, "fixture failed", "id", f.ID, "error", err)
				s = CheckerScore{ID: f.ID, Title: f.Job.JD.Title, Resume: f.Expected.Resume, Error: err.Error()}
			}
			s.DurationMS = time.Since(t0).Milliseconds()
			scores[i] = s
			r.log.InfoContext(gctx, "fixture scored", "id", f.ID, "links", s.PredictedLinks, "correct", s.CorrectLinks)
			return nil
		})
	}
	_ = g.Wait()

	return CheckerReport{
		Started:  start.UTC(),
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Model:    model,
		Revision: revision(),
		Checker:  r.cfg,
		Labels:   CheckerLabelsHash(fixtures),
		Totals:   checkerTotals(scores),
		Fixtures: scores,
		Metrics:  r.recorder.Summary(),
	}
}

// RescoreChecker scores the results stored in a previous report against
// the current labels, without calling any API. traces come from the
// previous run's trace files; without them retrieval recall reads 0.
func RescoreChecker(prev CheckerReport, fixtures []CheckerFixture, traces map[string]*domain.CheckTrace) (CheckerReport, error) {
	byID := map[string]CheckerScore{}
	for _, s := range prev.Fixtures {
		byID[s.ID] = s
	}
	var scores []CheckerScore
	for _, f := range fixtures {
		old, ok := byID[f.ID]
		if !ok || old.Error != "" || old.Result == nil {
			continue
		}
		tr := domain.CheckTrace{}
		if t := traces[f.ID]; t != nil {
			tr = *t
		}
		s, err := ScoreCheck(f, *old.Result, tr)
		if err != nil {
			return CheckerReport{}, err
		}
		s.Result, s.Trace, s.DurationMS = old.Result, traces[f.ID], old.DurationMS
		scores = append(scores, s)
	}
	rep := prev
	rep.Started = time.Now().UTC()
	rep.RescoredFrom = prev.Started.Format(time.RFC3339)
	rep.Labels = CheckerLabelsHash(fixtures)
	rep.Totals = checkerTotals(scores)
	rep.Fixtures = scores
	return rep, nil
}

// CheckerLabelsHash fingerprints the pair labels, the golden Requirement
// labels they use, and the Resumes.
func CheckerLabelsHash(fixtures []CheckerFixture) string {
	h := sha256.New()
	for _, f := range fixtures {
		exp, _ := json.Marshal(f.Expected)
		job, _ := json.Marshal(f.Job.Expected)
		fmt.Fprintf(h, "%s\n%s\n%s\n%s\n", f.ID, exp, job, f.Resume.Text)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func checkerTotals(scores []CheckerScore) CheckerTotals {
	t := CheckerTotals{Fixtures: len(scores), Coverage: map[string]CoverageTally{}, Confusion: map[string]map[string]int{}}
	var retrieved, labeledSP, retrievedSP, exact, near int
	var cov CoverageTally
	for _, s := range scores {
		if s.Error != "" {
			t.Failed++
			continue
		}
		t.LabeledPairs += s.LabeledPairs
		t.PredictedLinks += s.PredictedLinks
		t.CorrectLinks += s.CorrectLinks
		retrieved += s.Retrieved
		labeledSP += s.LabeledStrongPartial
		retrievedSP += s.RetrievedStrongPartial
		exact += s.StrengthExact
		near += s.StrengthNear
		for tier, c := range s.Coverage {
			agg := t.Coverage[tier]
			agg.Total += c.Total
			agg.Exact += c.Exact
			agg.Covered += c.Covered
			t.Coverage[tier] = agg
			cov.Total += c.Total
			cov.Exact += c.Exact
			cov.Covered += c.Covered
		}
		for w, row := range s.Confusion {
			if t.Confusion[w] == nil {
				t.Confusion[w] = map[string]int{}
			}
			for g, n := range row {
				t.Confusion[w][g] += n
			}
		}
	}
	t.RetrievalRecall = ratio(retrieved, t.LabeledPairs)
	t.RetrievalRecallStrongPartial = ratio(retrievedSP, labeledSP)
	t.LinkPrecision = ratio(t.CorrectLinks, t.PredictedLinks)
	t.LinkRecall = ratio(t.CorrectLinks, t.LabeledPairs)
	if p, r := t.LinkPrecision, t.LinkRecall; p+r > 0 {
		t.LinkF1 = 2 * p * r / (p + r)
	}
	t.StrengthExact = ratio(exact, t.CorrectLinks)
	t.StrengthNear = ratio(near, t.CorrectLinks)
	t.CoverageExact = ratio(cov.Exact, cov.Total)
	t.CoverageCovered = ratio(cov.Covered, cov.Total)
	return t
}

// Write saves the report as <dir>/<timestamp>.json and .md, with traces in
// <timestamp>-traces/, and returns the Markdown path.
func (r CheckerReport) Write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(dir, r.Started.Format("2006-01-02T15-04-05Z"))
	if r.RescoredFrom != "" {
		base += "-rescored"
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(base+".json", append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	for _, s := range r.Fixtures {
		if s.Trace == nil {
			continue
		}
		if err := os.MkdirAll(base+"-traces", 0o755); err != nil {
			return "", err
		}
		raw, err := json.MarshalIndent(s.Trace, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(base+"-traces", s.ID+".json"), append(raw, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	f, err := os.Create(base + ".md")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := r.WriteMarkdown(f); err != nil {
		return "", err
	}
	return base + ".md", nil
}

// LoadCheckTraces reads the trace files written next to a checker report.
func LoadCheckTraces(reportJSON string) (map[string]*domain.CheckTrace, error) {
	dir := strings.TrimSuffix(reportJSON, ".json") + "-traces"
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	out := map[string]*domain.CheckTrace{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var tr domain.CheckTrace
		if err := json.Unmarshal(raw, &tr); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = &tr
	}
	return out, nil
}

var coverageLevels = []string{"strong", "partial", "weak", "none"}

// WriteMarkdown renders the summary, the confusion matrix, and per-fixture
// disagreements.
func (r CheckerReport) WriteMarkdown(w io.Writer) error {
	t := r.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "# Resume Checker eval %s\n\n", r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "Model `%s` · revision `%s` · labels `%s` · %d fixtures (%d failed) · %s\n\n",
		r.Model, r.Revision, r.Labels, t.Fixtures, t.Failed, r.Duration)
	if r.RescoredFrom != "" {
		fmt.Fprintf(&b, "Rescored offline from the run of %s against the current labels.\n\n", r.RescoredFrom)
	}
	fmt.Fprintf(&b, "Checker: retrieval %s (K %d · floor %.3f · narrow %v · peel shortlist %d · noul threshold %.2f) · strength criteria %s · min evidence mass %.2f · gate %.2f (%s) · grading %s\n\n",
		r.Checker.RetrievalMode, r.Checker.RetrievalK, r.Checker.RetrievalFloor, r.Checker.NarrowSizes, r.Checker.PeelShortlist, r.Checker.NoulThreshold,
		r.Checker.StrengthCriteria, r.Checker.MinEvidenceMass, r.Checker.GateThreshold, r.Checker.GateWording, r.Checker.StrengthMode)

	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Coverage accuracy** | **%s** |\n", pct(t.CoverageExact))
	for _, tier := range []string{"required", "preferred", "mentioned"} {
		if c := t.Coverage[tier]; c.Total > 0 {
			fmt.Fprintf(&b, "| Coverage accuracy, %s | %s (%d/%d) |\n", tier, pct(ratio(c.Exact, c.Total)), c.Exact, c.Total)
		}
	}
	fmt.Fprintf(&b, "| Covered vs none agreement | %s |\n", pct(t.CoverageCovered))
	fmt.Fprintf(&b, "| Retrieval recall (all labels) | %s |\n", pct(t.RetrievalRecall))
	fmt.Fprintf(&b, "| Retrieval recall (strong + partial) | %s |\n", pct(t.RetrievalRecallStrongPartial))
	fmt.Fprintf(&b, "| Link precision | %s (%d/%d) |\n", pct(t.LinkPrecision), t.CorrectLinks, t.PredictedLinks)
	fmt.Fprintf(&b, "| Link recall | %s (%d/%d) |\n", pct(t.LinkRecall), t.CorrectLinks, t.LabeledPairs)
	fmt.Fprintf(&b, "| Link F1 | %s |\n", pct(t.LinkF1))
	fmt.Fprintf(&b, "| Strength exact / within one | %s / %s |\n", pct(t.StrengthExact), pct(t.StrengthNear))
	if c, ok := r.Metrics.Counters["jev.cost_micro_usd"]; ok {
		fmt.Fprintf(&b, "| Jev cost | $%.4f |\n", float64(c)/1e6)
	}
	fmt.Fprintf(&b, "| Jev calls | %d |\n\n", r.Metrics.Counters["jev.calls"])

	b.WriteString("## Coverage confusion (rows: label, columns: predicted)\n\n| | strong | partial | weak | none |\n|---|---|---|---|---|\n")
	for _, w := range coverageLevels {
		fmt.Fprintf(&b, "| %s |", w)
		for _, g := range coverageLevels {
			fmt.Fprintf(&b, " %d |", t.Confusion[w][g])
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Fixtures\n\n| Pair | Resume | Coverage | Link P | Link R | Time |\n|---|---|---|---|---|---|\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			fmt.Fprintf(&b, "| %s | %s | error: %s | | | |\n", s.Title, s.Resume, s.Error)
			continue
		}
		var total, exact int
		for _, c := range s.Coverage {
			total += c.Total
			exact += c.Exact
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %.1fs |\n", s.Title, s.Resume, pct(ratio(exact, total)),
			pct(ratio(s.CorrectLinks, s.PredictedLinks)), pct(ratio(s.CorrectLinks, s.LabeledPairs)), float64(s.DurationMS)/1000)
	}

	b.WriteString("\n## Disagreements\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			continue
		}
		fmt.Fprintf(&b, "\n### %s × %s\n\n", s.Resume, s.Title)
		writeNotes(&b, "Coverage wrong", s.CoverageErrors, false)
		writeNotes(&b, "Missed links", s.MissedLinks, true)
		writeNotes(&b, "False links", s.FalseLinks, true)
		writeNotes(&b, "Strength differs", s.StrengthDiffs, true)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeNotes(b *strings.Builder, title string, notes []PairNote, withUnit bool) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(b, "- **%s (%d):**\n", title, len(notes))
	for _, n := range notes {
		if withUnit {
			fmt.Fprintf(b, "  - `%s` ← %s \"%s\": want %s, got %s\n", n.Requirement, n.Unit, n.Text, n.Want, n.Got)
		} else {
			fmt.Fprintf(b, "  - `%s`: want %s, got %s\n", n.Requirement, n.Want, n.Got)
		}
	}
}
