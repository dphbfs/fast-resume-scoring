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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/fsutil"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/internal/port"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

// CheckerRunner runs the Resume Checker over the checker fixtures.
type CheckerRunner struct {
	checker  port.ResumeChecker
	baseline *Baseline // nil: no generative baseline arm
	recorder *metrics.Recorder
	log      *slog.Logger
	cfg      config.Checker
	tuning   *tuning.Tuning
}

// NewCheckerRunner builds a CheckerRunner. cfg is recorded in each report.
// A nil baseline runs the Jev arm only.
func NewCheckerRunner(checker port.ResumeChecker, baseline *Baseline, recorder *metrics.Recorder, log *slog.Logger, cfg config.Checker,
	t *tuning.Tuning) *CheckerRunner {
	return &CheckerRunner{checker: checker, baseline: baseline, recorder: recorder, log: log.With("component", "eval"), cfg: cfg, tuning: t}
}

// CheckerReport is one Resume Checker eval run.
type CheckerReport struct {
	Started      time.Time       `json:"started"`
	Duration     string          `json:"duration"`
	Model        string          `json:"model"`
	Revision     string          `json:"revision"`
	Tuning       string          `json:"tuning,omitempty"`
	Checker      config.Checker  `json:"checker"`
	Baseline     *BaselineConfig `json:"baseline,omitempty"`
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
	// FitError is the mean absolute difference between the predicted and
	// the labeled Fit Score over FitPairs; FitErrorMax the largest one.
	FitError    float64 `json:"fit_error"`
	FitErrorMax int     `json:"fit_error_max"`
	FitPairs    int     `json:"fit_pairs"`

	LabeledPairs   int `json:"labeled_pairs"`
	PredictedLinks int `json:"predicted_links"`
	CorrectLinks   int `json:"correct_links"`

	// JevMeanMS is the mean time of one Resume Checker call per pair.
	JevMeanMS int64 `json:"jev_mean_ms"`
	// Baseline compares the generative baseline with the Jev arm; nil when
	// the baseline did not run.
	Baseline *BaselineTotals `json:"baseline,omitempty"`
}

// BaselineTotals compare the two arms on the pairs where both produced a
// Fit Score (Pairs), each against the labeled Fit Score.
type BaselineTotals struct {
	Ran    int `json:"ran"`
	Failed int `json:"failed"`
	Pairs  int `json:"pairs"`

	FitError       float64 `json:"fit_error"`
	FitErrorMax    int     `json:"fit_error_max"`
	JevFitError    float64 `json:"jev_fit_error"`
	JevFitErrorMax int     `json:"jev_fit_error_max"`
	// FitBias is the mean signed error (predicted - labeled): above 0 the
	// baseline scores too high.
	FitBias    float64 `json:"fit_bias"`
	JevFitBias float64 `json:"jev_fit_bias"`

	MeanMS       int64 `json:"mean_ms"`
	InputTokens  int   `json:"input_tokens"`
	OutputTokens int   `json:"output_tokens"`
	// InputEstimated counts calls whose input tokens were estimated.
	InputEstimated int `json:"input_estimated"`
	// CostUSD sums the calls whose cost is known (CostKnown of Ran).
	CostUSD   float64 `json:"cost_usd"`
	CostKnown int     `json:"cost_known"`
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
			took := time.Since(t0)
			var s CheckerScore
			if err == nil {
				s, err = ScoreCheck(f, res, tr, r.tuning.FitWeights())
				s.Result, s.Trace = &res, &tr
				mu.Lock()
				model = res.Model
				mu.Unlock()
			}
			if err != nil {
				r.log.ErrorContext(gctx, "fixture failed", "id", f.ID, "error", err)
				s = CheckerScore{ID: f.ID, Title: f.Job.JD.Title, Resume: f.Expected.Resume, Error: err.Error()}
			}
			s.DurationMS = took.Milliseconds()
			if r.baseline != nil {
				b := r.baseline.Score(gctx, f.Job.JD.Text, f.Resume.Text)
				s.Baseline = &b
				if b.Error != "" {
					r.log.ErrorContext(gctx, "baseline failed", "id", f.ID, "error", b.Error)
				}
			}
			scores[i] = s
			r.log.InfoContext(gctx, "fixture scored", "id", f.ID, "links", s.PredictedLinks, "correct", s.CorrectLinks)
			return nil
		})
	}
	_ = g.Wait()

	var bcfg *BaselineConfig
	if r.baseline != nil {
		bcfg = &r.baseline.cfg
	}
	return CheckerReport{
		Started:  start.UTC(),
		Duration: time.Since(start).Round(time.Millisecond).String(),
		Model:    model,
		Revision: revision(),
		Tuning:   r.tuning.Hash,
		Checker:  r.cfg,
		Baseline: bcfg,
		Labels:   CheckerLabelsHash(fixtures),
		Totals:   checkerTotals(scores),
		Fixtures: scores,
		Metrics:  r.recorder.Summary(),
	}
}

// RescoreChecker scores the results stored in a previous report against
// the current labels, without calling any API. traces come from the
// previous run's trace files; without them retrieval recall reads 0.
func RescoreChecker(prev CheckerReport, fixtures []CheckerFixture, traces map[string]*domain.CheckTrace, t *tuning.Tuning) (CheckerReport, error) {
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
		s, err := ScoreCheck(f, *old.Result, tr, t.FitWeights())
		if err != nil {
			return CheckerReport{}, err
		}
		s.Result, s.Trace, s.DurationMS, s.Baseline = old.Result, traces[f.ID], old.DurationMS, old.Baseline
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
	var retrieved, labeledSP, retrievedSP, exact, near, fitErr int
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
		if s.FitGot != nil && s.FitWant != nil {
			d := max(*s.FitGot-*s.FitWant, *s.FitWant-*s.FitGot)
			fitErr += d
			t.FitErrorMax = max(t.FitErrorMax, d)
			t.FitPairs++
		}
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
	if t.FitPairs > 0 {
		t.FitError = float64(fitErr) / float64(t.FitPairs)
	}
	var jevMS int64
	for _, s := range scores {
		if s.Error == "" {
			jevMS += s.DurationMS
		}
	}
	if n := t.Fixtures - t.Failed; n > 0 {
		t.JevMeanMS = jevMS / int64(n)
	}
	t.Baseline = baselineTotals(scores)
	return t
}

// baselineTotals is nil when no fixture ran the baseline.
func baselineTotals(scores []CheckerScore) *BaselineTotals {
	var bt BaselineTotals
	var ms int64
	var errB, errJ, biasB, biasJ int
	for _, s := range scores {
		b := s.Baseline
		if b == nil {
			continue
		}
		bt.Ran++
		ms += b.DurationMS
		bt.InputTokens += b.InputTokens
		bt.OutputTokens += b.OutputTokens
		if b.InputTokensEstimated {
			bt.InputEstimated++
		}
		if b.CostUSD != nil {
			bt.CostUSD += *b.CostUSD
			bt.CostKnown++
		}
		if b.Error != "" {
			bt.Failed++
			continue
		}
		if s.Error != "" || s.FitWant == nil || s.FitGot == nil || b.Score == nil {
			continue
		}
		bt.Pairs++
		dB, dJ := *b.Score-*s.FitWant, *s.FitGot-*s.FitWant
		biasB += dB
		biasJ += dJ
		errB += abs(dB)
		errJ += abs(dJ)
		bt.FitErrorMax = max(bt.FitErrorMax, abs(dB))
		bt.JevFitErrorMax = max(bt.JevFitErrorMax, abs(dJ))
	}
	if bt.Ran == 0 {
		return nil
	}
	bt.MeanMS = ms / int64(bt.Ran)
	if n := float64(bt.Pairs); n > 0 {
		bt.FitError, bt.JevFitError = float64(errB)/n, float64(errJ)/n
		bt.FitBias, bt.JevFitBias = float64(biasB)/n, float64(biasJ)/n
	}
	return &bt
}

func abs(n int) int {
	return max(n, -n)
}

// Write saves the report as <dir>/<timestamp>.json and .md, with traces in
// <timestamp>-traces/, and returns the Markdown path.
func (r CheckerReport) Write(dir string) (string, error) {
	base := filepath.Join(dir, r.Started.Format("2006-01-02T15-04-05Z"))
	if r.RescoredFrom != "" {
		// Named after the source run, so rescoring several runs at once
		// cannot overwrite one another.
		from, err := time.Parse(time.RFC3339, r.RescoredFrom)
		if err != nil {
			return "", fmt.Errorf("rescored_from %q: %w", r.RescoredFrom, err)
		}
		base = filepath.Join(dir, from.UTC().Format("2006-01-02T15-04-05Z")+"-rescored")
	}
	if err := fsutil.WriteJSONAtomic(base+".json", r); err != nil {
		return "", err
	}
	for _, s := range r.Fixtures {
		if s.Trace == nil {
			continue
		}
		if err := fsutil.WriteJSONAtomic(filepath.Join(base+"-traces", s.ID+".json"), s.Trace); err != nil {
			return "", err
		}
	}
	var b strings.Builder
	if err := r.WriteMarkdown(&b); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(base+".md", []byte(b.String())); err != nil {
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
	fmt.Fprintf(&b, "Checker: retrieval K %d · floor %.3f · narrow %v · min evidence mass %.2f · gate %.2f · skip capped grading %v · gate first %v\n\n",
		r.Checker.RetrievalK, r.Checker.RetrievalFloor, r.Checker.NarrowSizes, r.Checker.MinEvidenceMass,
		r.Checker.GateThreshold, r.Checker.SkipCappedGrading, r.Checker.GateFirst)

	b.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Coverage accuracy** | **%s** |\n", pct(t.CoverageExact))
	for _, tier := range []string{"required", "preferred", "mentioned"} {
		if c := t.Coverage[tier]; c.Total > 0 {
			fmt.Fprintf(&b, "| Coverage accuracy, %s | %s (%d/%d) |\n", tier, pct(ratio(c.Exact, c.Total)), c.Exact, c.Total)
		}
	}
	fmt.Fprintf(&b, "| Covered vs none agreement | %s |\n", pct(t.CoverageCovered))
	fmt.Fprintf(&b, "| Fit Score error, mean / max (points) | %.1f / %d |\n", t.FitError, t.FitErrorMax)
	fmt.Fprintf(&b, "| Retrieval recall (all labels) | %s |\n", pct(t.RetrievalRecall))
	fmt.Fprintf(&b, "| Retrieval recall (strong + partial) | %s |\n", pct(t.RetrievalRecallStrongPartial))
	fmt.Fprintf(&b, "| Link precision | %s (%d/%d) |\n", pct(t.LinkPrecision), t.CorrectLinks, t.PredictedLinks)
	fmt.Fprintf(&b, "| Link recall | %s (%d/%d) |\n", pct(t.LinkRecall), t.CorrectLinks, t.LabeledPairs)
	fmt.Fprintf(&b, "| Link F1 | %s |\n", pct(t.LinkF1))
	fmt.Fprintf(&b, "| Strength exact / within one | %s / %s |\n", pct(t.StrengthExact), pct(t.StrengthNear))
	if c, ok := r.Metrics.Counters["jev.cost_micro_usd"]; ok {
		fmt.Fprintf(&b, "| Jev cost | $%.4f |\n", float64(c)/1e6)
	}
	fmt.Fprintf(&b, "| Jev calls | %d |\n", r.Metrics.Counters["jev.calls"])
	fmt.Fprintf(&b, "| Jev input tokens (retrieval / strength) | %d (%d / %d) |\n\n", r.Metrics.Counters["jev.input_tokens"],
		r.Metrics.Counters["checker.retrieval.input_tokens"], r.Metrics.Counters["checker.strength.input_tokens"])

	writeBaseline(&b, r)
	b.WriteString("## Coverage confusion (rows: label, columns: predicted)\n\n| | strong | partial | weak | none |\n|---|---|---|---|---|\n")
	for _, w := range coverageLevels {
		fmt.Fprintf(&b, "| %s |", w)
		for _, g := range coverageLevels {
			fmt.Fprintf(&b, " %d |", t.Confusion[w][g])
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Fixtures\n\n| Pair | Resume | Coverage | Fit (predicted / labeled) | Link P | Link R | Time |\n|---|---|---|---|---|---|---|\n")
	for _, s := range r.Fixtures {
		if s.Error != "" {
			fmt.Fprintf(&b, "| %s | %s | error: %s | | | | |\n", s.Title, s.Resume, s.Error)
			continue
		}
		var total, exact int
		for _, c := range s.Coverage {
			total += c.Total
			exact += c.Exact
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s / %s | %s | %s | %.1fs |\n", s.Title, s.Resume, pct(ratio(exact, total)),
			fitText(s.FitGot), fitText(s.FitWant), pct(ratio(s.CorrectLinks, s.PredictedLinks)), pct(ratio(s.CorrectLinks, s.LabeledPairs)), float64(s.DurationMS)/1000)
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

// writeBaseline renders the Jev vs generative comparison, when it ran.
func writeBaseline(b *strings.Builder, r CheckerReport) {
	bt := r.Totals.Baseline
	if bt == nil {
		return
	}
	model := ""
	if r.Baseline != nil {
		model = r.Baseline.Model
	}
	fmt.Fprintf(b, "## Generative baseline (`%s`)\n\n", model)
	b.WriteString("Reactive Resume's one-prompt match score against the Resume Checker's Fit Score, both measured against the Fit Score of the labeled Coverage. ")
	b.WriteString("Jev cost covers the Resume Checker only: the input Requirements are golden labels, so the Requirement Extractor (once per Job Description) is not included.\n\n")
	fmt.Fprintf(b, "| | Jev | Generative |\n|---|---|---|\n")
	fmt.Fprintf(b, "| Fit error vs labeled, mean / max (%d pairs) | %.1f / %d | %.1f / %d |\n", bt.Pairs, bt.JevFitError, bt.JevFitErrorMax, bt.FitError, bt.FitErrorMax)
	fmt.Fprintf(b, "| Fit bias (mean predicted - labeled) | %+.1f | %+.1f |\n", bt.JevFitBias, bt.FitBias)
	fmt.Fprintf(b, "| Mean time per pair | %.1fs | %.1fs |\n", float64(r.Totals.JevMeanMS)/1000, float64(bt.MeanMS)/1000)
	jevCost, genCost := "–", "–"
	if c, ok := r.Metrics.Counters["jev.cost_micro_usd"]; ok && r.Totals.Fixtures > 0 {
		jevCost = fmt.Sprintf("$%.4f", float64(c)/1e6/float64(r.Totals.Fixtures))
	}
	if bt.CostKnown > 0 {
		genCost = fmt.Sprintf("$%.4f", bt.CostUSD/float64(bt.CostKnown))
	}
	fmt.Fprintf(b, "| Cost per pair | %s | %s |\n", jevCost, genCost)
	if bt.Ran > 0 {
		est := ""
		if bt.InputEstimated > 0 {
			est = fmt.Sprintf(" (input estimated from prompt length on %d/%d calls)", bt.InputEstimated, bt.Ran)
		}
		fmt.Fprintf(b, "| Generative tokens per pair (in / out) | | %d / %d%s |\n", bt.InputTokens/bt.Ran, bt.OutputTokens/bt.Ran, est)
	}
	fmt.Fprintf(b, "| Failed | %d/%d | %d/%d |\n\n", r.Totals.Failed, r.Totals.Fixtures, bt.Failed, bt.Ran)

	b.WriteString("| Pair | Resume | Labeled | Jev | Generative | Labeled Gaps | Generative gaps |\n|---|---|---|---|---|---|---|\n")
	for _, s := range r.Fixtures {
		g := s.Baseline
		if g == nil {
			continue
		}
		gen, gaps := fitText(g.Score), strings.Join(g.Gaps, "; ")
		if g.Error != "" {
			gen, gaps = "error", g.Error
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", s.Title, s.Resume, fitText(s.FitWant), fitText(s.FitGot), gen,
			cell(strings.Join(s.GapsWant, "; ")), cell(gaps))
	}
	b.WriteString("\n")
}

// cell keeps free text from breaking a Markdown table row.
func cell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}

func fitText(score *int) string {
	if score == nil {
		return "–"
	}
	return strconv.Itoa(*score)
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
